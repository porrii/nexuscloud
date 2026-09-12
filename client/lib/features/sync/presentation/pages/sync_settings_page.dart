import 'dart:async';

import 'package:flutter/material.dart';
import 'package:path/path.dart' as p;

import '../../../../core/di/service_locator.dart';
import '../../../../core/storage/window_preferences_store.dart';
import '../../../../core/widgets/confirm_dialog.dart';
import '../../../../core/window/app_tray_service.dart';
import '../../../../core/window/launch_at_startup_service.dart';
import '../../domain/entities/auto_sync_settings.dart';
import '../../domain/entities/pair_sync_outcome.dart';
import '../../domain/entities/pending_delete.dart';
import '../../domain/entities/sync_direction.dart';
import '../../domain/entities/sync_pair.dart';
import '../../domain/entities/sync_pair_config.dart';
import '../../domain/repositories/sync_config_repository.dart';
import '../../domain/services/auto_sync_scheduler.dart';
import '../../domain/services/local_change_watcher_service.dart';
import '../../domain/services/multi_pair_sync_coordinator.dart';
import '../../domain/services/sync_engine.dart';
import '../widgets/pair_edit_dialog.dart';
import '../widgets/pair_sync_result_card.dart';

/// Intervalos de sincronización automática ofrecidos en el desplegable.
/// 5 minutos es el mínimo -- por debajo de eso, el coste de recorrer el
/// árbol remoto entero en cada tick (ADR-011, sin estado incremental) deja
/// de ser razonable para carpetas con muchos archivos.
const _autoSyncIntervalOptions = [5, 15, 30, 60];

/// Configura la lista de pares carpeta-remota/carpeta-local a sincronizar
/// (slice A ADR-011 -- un único par; slice 15 ADR-014 -- varios, cada uno
/// con su propio sentido), dispara sincronizaciones manuales (todos los
/// pares o solo uno), y desde el slice 8 permite activar una sincronización
/// automática global que cubre todos los pares mientras la app esté abierta.
class SyncSettingsPage extends StatefulWidget {
  const SyncSettingsPage({super.key});

  @override
  State<SyncSettingsPage> createState() => _SyncSettingsPageState();
}

class _SyncSettingsPageState extends State<SyncSettingsPage> {
  final SyncConfigRepository _configRepository = sl<SyncConfigRepository>();
  final SyncEngine _syncEngine = sl<SyncEngine>();
  final MultiPairSyncCoordinator _coordinator = sl<MultiPairSyncCoordinator>();
  final AutoSyncScheduler _autoSyncScheduler = sl<AutoSyncScheduler>();
  final LocalChangeWatcherService _watcherService = sl<LocalChangeWatcherService>();
  final AppTrayService _trayService = sl<AppTrayService>();
  final LaunchAtStartupService _launchAtStartupService =
      sl<LaunchAtStartupService>();
  // Directo a `WindowPreferencesStore`, no a través de `AppTrayService` ni
  // `LaunchAtStartupService`: "iniciar minimizado" no es dueño de ninguno
  // de los dos (ver el plan del slice 10 sobre por qué la coordinación
  // entre ambos vive aquí, en la página, y no dentro de un servicio).
  final WindowPreferencesStore _windowPreferencesStore =
      sl<WindowPreferencesStore>();

  List<SyncPairConfig> _pairs = [];

  bool _syncing = false;
  String? _statusMessage;
  List<PairSyncOutcome>? _lastOutcomes;

  bool _autoSyncEnabled = false;
  int _autoSyncIntervalMinutes = _autoSyncIntervalOptions[1];
  ({DateTime at, String summary})? _lastAutoOutcome;

  bool _watchLocalChangesEnabled = false;

  /// Instantánea de `AppTrayService.minimizeToTrayOnClose` (slice 9) --
  /// mismo criterio que `_engineBusy`: el servicio ya lo cachea tras su
  /// propio `init()`, así que leerlo aquí no necesita otro `await`.
  bool _minimizeToTrayOnClose = false;

  /// A diferencia de `_minimizeToTrayOnClose`, no hay valor cacheado
  /// síncrono disponible -- `launchAtStartup.isEnabled()` relee el
  /// registro de Windows de verdad en cada llamada (es la fuente de
  /// verdad, no `shared_preferences`), así que empieza en `false` hasta
  /// que `_loadLaunchAtStartupSettings` resuelve.
  bool _launchAtStartupEnabled = false;
  bool _startMinimized = false;
  String? _launchAtStartupError;

  /// Reflejo de `SyncEngine.onBusyChanged` -- true mientras CUALQUIER par
  /// tenga una sincronización en curso (slice 15: el motor la mantiene por
  /// par, ver ADR-014), la haya arrancado un botón de esta página o un tick
  /// de `_autoSyncScheduler`. Las acciones de esta página se deshabilitan
  /// también con esto para que un clic nunca pueda unirse en silencio a un
  /// tick automático ya en curso para un par distinto al que se ve en
  /// pantalla.
  bool _engineBusy = false;

  late final StreamSubscription<List<PairSyncOutcome>> _autoResultSubscription;
  late final StreamSubscription<String> _autoStatusSubscription;
  late final StreamSubscription<bool> _busySubscription;

  @override
  void initState() {
    super.initState();
    // Las tres suscripciones se crean aquí, antes de cualquier `await`,
    // para no perder ningún evento emitido justo tras montar la página --
    // los `StreamController.broadcast()` de por debajo no repiten nada a
    // quien se suscribe tarde.
    _autoResultSubscription = _autoSyncScheduler.onResult.listen(
      _handleAutoResult,
    );
    _autoStatusSubscription = _autoSyncScheduler.onStatus.listen(
      _handleAutoStatus,
    );
    _busySubscription = _syncEngine.onBusyChanged.listen(_handleBusyChanged);
    // Instantánea del estado actual -- el stream de arriba solo avisa de
    // cambios futuros, no repite el último valor a quien llega tarde.
    _engineBusy = _syncEngine.isRunning;
    _minimizeToTrayOnClose = _trayService.minimizeToTrayOnClose;

    _loadPairs();
    _loadAutoSyncSettings();
    _loadWatchLocalChangesSetting();
    _loadLaunchAtStartupSettings();
  }

  Future<void> _loadPairs() async {
    final pairs = await _configRepository.readPairs();
    if (!mounted) return;
    setState(() => _pairs = pairs);
  }

  @override
  void dispose() {
    _autoResultSubscription.cancel();
    _autoStatusSubscription.cancel();
    _busySubscription.cancel();
    super.dispose();
  }

  Future<void> _loadAutoSyncSettings() async {
    final settings = await _configRepository.readAutoSync();
    final outcome = await _configRepository.readLastAutoSyncOutcome();
    if (!mounted) return;
    setState(() {
      _autoSyncEnabled = settings.enabled;
      _autoSyncIntervalMinutes = settings.intervalMinutes;
      _lastAutoOutcome = outcome;
    });
  }

  Future<void> _loadWatchLocalChangesSetting() async {
    final enabled = await _configRepository.readWatchLocalChanges();
    if (!mounted) return;
    setState(() => _watchLocalChangesEnabled = enabled);
  }

  /// El invariante de "iniciar minimizado no puede quedar huérfano" se
  /// cierra en DOS sitios (ver el plan del slice 10): aquí, al cargar la
  /// página, y en `_setLaunchAtStartupEnabled` al desactivar el interruptor
  /// a mano. Hace falta aquí también porque `isEnabled()` puede volverse
  /// `false` sin que esta app se entere -- p.ej. si el usuario lo
  /// desactiva desde el Administrador de tareas → "Aplicaciones de
  /// inicio", que no pasa por ningún código de esta app en absoluto.
  Future<void> _loadLaunchAtStartupSettings() async {
    final enabled = await _launchAtStartupService.isEnabled();
    final startMinimized = await _windowPreferencesStore.readStartMinimized();
    if (!enabled && startMinimized) {
      await _windowPreferencesStore.saveStartMinimized(false);
    }
    if (!mounted) return;
    setState(() {
      _launchAtStartupEnabled = enabled;
      _startMinimized = enabled && startMinimized;
    });
  }

  void _handleAutoResult(List<PairSyncOutcome> outcomes) {
    if (!mounted) return;
    // Mismo campo que ya pinta el bloque "Resultado" de más abajo -- desde
    // el punto de vista del usuario es el mismo concepto, disparado a mano
    // o solo, no hace falta una sección aparte. A diferencia de un sync
    // manual, un tick automático NUNCA abre el diálogo de confirmación de
    // borrados por su cuenta -- ver `_runSync`.
    setState(() => _lastOutcomes = _mergeOutcomes(outcomes));
  }

  void _handleAutoStatus(String status) {
    if (!mounted || _syncing) return;
    setState(() => _statusMessage = status);
  }

  void _handleBusyChanged(bool busy) {
    if (!mounted) return;
    setState(() {
      _engineBusy = busy;
      if (!busy && !_syncing) {
        // Un tick automático (o, en el límite, un sync manual disparado
        // desde otra ventana) acaba de terminar -- limpia el mensaje de
        // progreso para no dejarlo colgado en pantalla.
        _statusMessage = null;
      }
    });
  }

  /// Combina [newOutcomes] con lo que ya hubiera en [_lastOutcomes] --
  /// sincronizar solo UN par (o un tick automático que, en el límite,
  /// cubriera menos pares que los configurados) no debe hacer desaparecer
  /// el último resultado de los demás. El orden final sigue el de [_pairs]
  /// actual; un par que ya no está en la lista se descarta.
  List<PairSyncOutcome> _mergeOutcomes(List<PairSyncOutcome> newOutcomes) {
    final byKey = {
      for (final o in _lastOutcomes ?? const <PairSyncOutcome>[])
        o.pair.stableKey: o,
      for (final o in newOutcomes) o.pair.stableKey: o,
    };
    return [
      for (final cfg in _pairs)
        if (byKey.containsKey(cfg.pair.stableKey)) byKey[cfg.pair.stableKey]!,
    ];
  }

  Future<void> _addPair() async {
    final config = await showPairEditDialog(context);
    if (config == null || !mounted) return;
    setState(() => _pairs = [..._pairs, config]);
    await _configRepository.savePairs(_pairs);
    await _watcherService.updatePairs(_pairs);
  }

  Future<void> _editPair(int index) async {
    final config = await showPairEditDialog(context, initial: _pairs[index]);
    if (config == null || !mounted) return;
    setState(() {
      _pairs = [..._pairs]..[index] = config;
    });
    await _configRepository.savePairs(_pairs);
    await _watcherService.updatePairs(_pairs);
  }

  Future<void> _removePair(int index) async {
    setState(() {
      _pairs = [..._pairs]..removeAt(index);
      _lastOutcomes = _lastOutcomes?.where(
        (o) => _pairs.any((c) => c.pair.stableKey == o.pair.stableKey),
      ).toList();
    });
    await _configRepository.savePairs(_pairs);
    await _watcherService.updatePairs(_pairs);
  }

  Future<void> _syncAll() => _runSync(_pairs);

  Future<void> _syncOnePair(SyncPairConfig config) => _runSync([config]);

  /// Compartido entre "Sincronizar todo ahora", el botón de sincronizar un
  /// único par, y la re-sincronización que dispara
  /// `_confirmPendingDeletesFor` tras confirmar un lote de borrados de un
  /// par concreto (ADR-013/ADR-014) -- mismo flujo en los tres casos, solo
  /// cambia la lista de pares a procesar.
  Future<void> _runSync(
    List<SyncPairConfig> toSync, {
    Map<String, Set<String>>? confirmedDeletePathsByPair,
  }) async {
    if (toSync.isEmpty) return;
    setState(() {
      _syncing = true;
      _statusMessage = 'Preparando...';
    });

    final outcomes = await _coordinator.syncAllNow(
      toSync,
      confirmedDeletePathsByPair: confirmedDeletePathsByPair,
      onStatus: (pair, status) {
        if (!mounted) return;
        setState(() => _statusMessage = '${p.basename(pair.localPath)}: $status');
      },
    );
    if (!mounted) return;
    setState(() {
      _syncing = false;
      _statusMessage = null;
      _lastOutcomes = _mergeOutcomes(outcomes);
    });

    // Solo un sync disparado desde aquí (nunca un tick automático, que no
    // pasa por `_runSync`) abre el diálogo de confirmación, uno por par si
    // hiciera falta -- ver `PairSyncResultCard`/el botón "Revisar y
    // confirmar borrados" para el caso de un lote que llegó de un tick con
    // la página ya abierta.
    for (final outcome in outcomes) {
      final pending = outcome.result?.pendingDeletes;
      if (pending != null && pending.isNotEmpty) {
        await _confirmPendingDeletesFor(outcome.pair, pending);
      }
    }
  }

  /// Muestra la lista real de borrados detectados para [pair] (nunca solo
  /// una cifra) y, si el usuario confirma, vuelve a sincronizar ESE par
  /// pasando sus claves -- ADR-013, guarda anti-"borrado masivo". Cancelar
  /// no hace nada más: el mismo lote reaparecerá como pendiente en la
  /// próxima sincronización de ese par.
  Future<void> _confirmPendingDeletesFor(
    SyncPair pair,
    List<PendingDelete> pending,
  ) async {
    if (!mounted || pending.isEmpty) return;

    final details = pending
        .map((p) {
          final hint = p.direction == DeleteDirection.toRemote
              ? 'se borraría en el servidor (papelera)'
              : 'se movería a la papelera local';
          return '${p.displayPath} -- $hint';
        })
        .join('\n');

    final confirmed = await showConfirmDialog(
      context,
      title: 'Confirmar ${pending.length} borrados',
      message: '${pair.remotePath} → ${pair.localPath}\n\n'
          'Estos archivos desaparecieron de un lado desde la última '
          'sincronización. Nada se borra para siempre -- van a una '
          'papelera, recuperable. Revísalos antes de confirmar:\n\n$details',
      confirmLabel: 'Borrar ${pending.length}',
      danger: true,
    );
    if (!confirmed || !mounted) return;

    // El par pudo editarse/quitarse entre el sync y esta confirmación; si
    // ya no está en la lista, se re-sincroniza igual con `Ambos` (el único
    // sentido que genera borrados pendientes) para no perder la
    // confirmación que el usuario acaba de dar.
    SyncPairConfig? config;
    for (final c in _pairs) {
      if (c.pair.stableKey == pair.stableKey) config = c;
    }
    config ??= SyncPairConfig(pair: pair, direction: SyncDirection.both);

    await _runSync(
      [config],
      confirmedDeletePathsByPair: {
        pair.stableKey: {for (final d in pending) d.key},
      },
    );
  }

  @override
  Widget build(BuildContext context) {
    final syncDisabled = _syncing || _engineBusy;
    return Scaffold(
      appBar: AppBar(title: const Text('Sincronización')),
      // SingleChildScrollView, no Padding a secas: con auto-sync activado
      // (desplegable de intervalo visible), varios pares en la lista y el
      // interruptor de bandeja del slice 9, el contenido puede superar la
      // altura de una ventana de 720px real, y sin scroll eso desborda en
      // vez de recortarse.
      body: SingleChildScrollView(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 520),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(
                  'Sincroniza una o varias carpetas remotas con carpetas '
                  'locales. Cada carpeta añadida tiene su propio sentido: '
                  '"Descargar" solo trae del servidor, "Subir" solo envía, '
                  '"Ambos" reconcilia los dos lados. En "Descargar"/"Subir" '
                  'nada se borra nunca: un archivo que falte en el lado no '
                  'tocado se vuelve a traer del otro. En "Ambos", si un '
                  'archivo cambió en los dos sitios, no se sobrescribe nada '
                  '-- se deja una copia "(conflicto ...)" al lado; y si lo '
                  'borras en un lado, se borra también en el otro (a una '
                  'papelera, recuperable), salvo que sean muchos de golpe, '
                  'en cuyo caso se te pide confirmar antes viendo la lista '
                  'real. "Sincronizar todo ahora" recorre todas las '
                  'carpetas configuradas; cada una también se puede '
                  'sincronizar sola. La sincronización automática, si la '
                  'activas más abajo, cubre todas por igual mientras la '
                  'app esté abierta.',
                  style: Theme.of(context).textTheme.bodyMedium,
                ),
                const SizedBox(height: 24),
                if (_pairs.isEmpty)
                  const Padding(
                    padding: EdgeInsets.symmetric(vertical: 8),
                    child: Text('Ninguna carpeta configurada todavía.'),
                  )
                else
                  for (var i = 0; i < _pairs.length; i++) ...[
                    _PairRow(
                      config: _pairs[i],
                      disabled: syncDisabled,
                      onSync: () => _syncOnePair(_pairs[i]),
                      onEdit: () => _editPair(i),
                      onRemove: () => _removePair(i),
                    ),
                    const Divider(height: 1),
                  ],
                const SizedBox(height: 16),
                OutlinedButton(
                  onPressed: syncDisabled ? null : _addPair,
                  child: const Text('Añadir carpeta a sincronizar'),
                ),
                const SizedBox(height: 16),
                FilledButton(
                  onPressed: (syncDisabled || _pairs.isEmpty) ? null : _syncAll,
                  child: _syncing
                      ? const SizedBox(
                          width: 20,
                          height: 20,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Text('Sincronizar todo ahora'),
                ),
                if (_statusMessage != null) ...[
                  const SizedBox(height: 16),
                  Text(
                    _statusMessage!,
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ],
                const SizedBox(height: 24),
                const Divider(),
                SwitchListTile(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('Sincronizar automáticamente'),
                  subtitle: const Text(
                    'Repite la sincronización de todas las carpetas sola '
                    'mientras la app esté abierta.',
                  ),
                  value: _autoSyncEnabled,
                  onChanged: _setAutoSyncEnabled,
                ),
                if (_autoSyncEnabled) ...[
                  Row(
                    children: [
                      const Text('Cada'),
                      const SizedBox(width: 12),
                      DropdownButton<int>(
                        value: _autoSyncIntervalMinutes,
                        items: [
                          for (final minutes in _autoSyncIntervalOptions)
                            DropdownMenuItem(
                              value: minutes,
                              child: Text('$minutes minutos'),
                            ),
                        ],
                        onChanged: (minutes) {
                          if (minutes != null) _setAutoSyncInterval(minutes);
                        },
                      ),
                    ],
                  ),
                ],
                const SizedBox(height: 8),
                const Divider(),
                SwitchListTile(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('Vigilar cambios locales'),
                  subtitle: const Text(
                    'Sincroniza sola, poco después de guardar o borrar un '
                    'archivo en una carpeta con sentido "Subir" o "Ambos". '
                    'No sustituye al reloj ni al botón manual, los '
                    'complementa.',
                  ),
                  value: _watchLocalChangesEnabled,
                  onChanged: _setWatchLocalChangesEnabled,
                ),
                const SizedBox(height: 8),
                const Divider(),
                SwitchListTile(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('Minimizar a la bandeja al cerrar'),
                  subtitle: const Text(
                    'La X esconde la ventana en vez de cerrar la app -- así '
                    'la sincronización automática sigue corriendo. El icono '
                    'de la bandeja del sistema deja volver a abrirla o salir '
                    'de verdad.',
                  ),
                  value: _minimizeToTrayOnClose,
                  onChanged: _setMinimizeToTrayOnClose,
                ),
                const SizedBox(height: 8),
                const Divider(),
                SwitchListTile(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('Arrancar NexusCloud con Windows'),
                  subtitle: const Text(
                    'Se abre sola al iniciar sesión en Windows -- así la '
                    'sincronización automática puede estar activa incluso '
                    'tras reiniciar el equipo.',
                  ),
                  value: _launchAtStartupEnabled,
                  onChanged: _setLaunchAtStartupEnabled,
                ),
                if (_launchAtStartupError != null) ...[
                  const SizedBox(height: 8),
                  Text(
                    _launchAtStartupError!,
                    style: TextStyle(
                      color: Theme.of(context).colorScheme.error,
                    ),
                  ),
                ],
                if (_launchAtStartupEnabled) ...[
                  SwitchListTile(
                    contentPadding: EdgeInsets.zero,
                    title: const Text('Iniciar minimizado en la bandeja'),
                    subtitle: const Text(
                      'Al arrancar con Windows, empieza escondida en la '
                      'bandeja del sistema en vez de mostrar la ventana.',
                    ),
                    value: _startMinimized,
                    onChanged: _setStartMinimized,
                  ),
                ],
                if (_lastOutcomes != null && _lastOutcomes!.isNotEmpty) ...[
                  const SizedBox(height: 24),
                  const Divider(),
                  const SizedBox(height: 8),
                  Text('Resultado', style: Theme.of(context).textTheme.titleSmall),
                  const SizedBox(height: 8),
                  for (final outcome in _lastOutcomes!)
                    PairSyncResultCard(
                      outcome: outcome,
                      disabled: syncDisabled,
                      onConfirmPendingDeletes: (pending) =>
                          _confirmPendingDeletesFor(outcome.pair, pending),
                    ),
                ] else if (_lastAutoOutcome != null) ...[
                  // Sin ningún resultado todavía en esta sesión de la
                  // página (nunca se pulsó un botón de sync ni corrió
                  // ningún tick mientras estaba abierta) pero sí hay un
                  // intento automático de una sesión anterior -- solo un
                  // resumen de texto (ver por qué en SyncConfigRepository),
                  // no el desglose completo de arriba.
                  const SizedBox(height: 24),
                  const Divider(),
                  const SizedBox(height: 8),
                  Text(
                    'Última sincronización automática: '
                    '${_lastAutoOutcome!.summary} '
                    '(${_lastAutoOutcome!.at.toLocal()})',
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }

  Future<void> _setAutoSyncEnabled(bool enabled) async {
    setState(() => _autoSyncEnabled = enabled);
    await _autoSyncScheduler.updateSettings(
      AutoSyncSettings(
        enabled: enabled,
        intervalMinutes: _autoSyncIntervalMinutes,
      ),
    );
  }

  Future<void> _setAutoSyncInterval(int minutes) async {
    setState(() => _autoSyncIntervalMinutes = minutes);
    await _autoSyncScheduler.updateSettings(
      AutoSyncSettings(enabled: _autoSyncEnabled, intervalMinutes: minutes),
    );
  }

  Future<void> _setWatchLocalChangesEnabled(bool value) async {
    setState(() => _watchLocalChangesEnabled = value);
    await _watcherService.setEnabled(value);
  }

  Future<void> _setMinimizeToTrayOnClose(bool value) async {
    setState(() => _minimizeToTrayOnClose = value);
    await _trayService.updateMinimizeToTrayOnClose(value);
  }

  /// A diferencia de los demás interruptores de esta página, envuelve la
  /// llamada en `try/catch` (ver el plan del slice 10): a diferencia de
  /// `shared_preferences`/el scheduler en memoria que respaldan los otros,
  /// esto son dos escrituras de registro Win32 no atómicas vía FFI cruda,
  /// que sí pueden lanzar -- y el booleano que devolvería `enable()`/
  /// `disable()` no serviría para detectar un fallo aunque se comprobara
  /// (siempre `true` en Windows sin MSIX, confirmado en su código fuente).
  Future<void> _setLaunchAtStartupEnabled(bool value) async {
    final previous = _launchAtStartupEnabled;
    setState(() {
      _launchAtStartupEnabled = value;
      _launchAtStartupError = null;
      // Mismo invariante que en la carga: si se desactiva, "iniciar
      // minimizado" no puede quedar activado sin que el interruptor que
      // lo controla siga visible.
      if (!value) _startMinimized = false;
    });
    try {
      await _launchAtStartupService.setEnabled(value);
      if (!value) {
        await _windowPreferencesStore.saveStartMinimized(false);
      }
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _launchAtStartupEnabled = previous;
        _launchAtStartupError =
            'No se pudo cambiar el ajuste de arranque con Windows.';
      });
    }
  }

  Future<void> _setStartMinimized(bool value) async {
    setState(() => _startMinimized = value);
    await _windowPreferencesStore.saveStartMinimized(value);
  }
}

/// Una fila de la lista de pares configurados: rutas + dirección, con
/// acciones de sincronizar solo este/editar/quitar -- mismo lenguaje visual
/// que las filas de `FileBrowserPage`/`MySharesPage` (icono de acción a la
/// derecha de cada fila).
class _PairRow extends StatelessWidget {
  const _PairRow({
    required this.config,
    required this.disabled,
    required this.onSync,
    required this.onEdit,
    required this.onRemove,
  });

  final SyncPairConfig config;
  final bool disabled;
  final VoidCallback onSync;
  final VoidCallback onEdit;
  final VoidCallback onRemove;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: Row(
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  '${config.pair.remotePath} → ${config.pair.localPath}',
                  overflow: TextOverflow.ellipsis,
                ),
                Text(
                  config.direction.label,
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ],
            ),
          ),
          IconButton(
            icon: const Icon(Icons.sync),
            tooltip: 'Sincronizar solo esta carpeta',
            onPressed: disabled ? null : onSync,
          ),
          IconButton(
            icon: const Icon(Icons.edit_outlined),
            tooltip: 'Editar',
            onPressed: disabled ? null : onEdit,
          ),
          IconButton(
            icon: const Icon(Icons.delete_outline),
            tooltip: 'Quitar de la lista',
            onPressed: disabled ? null : onRemove,
          ),
        ],
      ),
    );
  }
}
