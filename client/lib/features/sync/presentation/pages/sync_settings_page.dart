import 'dart:async';

import 'package:file_selector/file_selector.dart';
import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/network/api_exception.dart';
import '../../../../core/storage/window_preferences_store.dart';
import '../../../../core/window/app_tray_service.dart';
import '../../../../core/window/launch_at_startup_service.dart';
import '../../domain/entities/auto_sync_settings.dart';
import '../../domain/entities/sync_direction.dart';
import '../../domain/entities/sync_pair.dart';
import '../../domain/entities/sync_result.dart';
import '../../domain/repositories/sync_config_repository.dart';
import '../../domain/services/auto_sync_scheduler.dart';
import '../../domain/services/sync_engine.dart';

/// Intervalos de sincronización automática ofrecidos en el desplegable.
/// 5 minutos es el mínimo -- por debajo de eso, el coste de recorrer el
/// árbol remoto entero en cada tick (ADR-011, sin estado incremental) deja
/// de ser razonable para carpetas con muchos archivos.
const _autoSyncIntervalOptions = [5, 15, 30, 60];

/// Configura el único par carpeta-remota/carpeta-local (slice A, ADR-011),
/// dispara una sincronización manual, y desde el slice 8 permite activar
/// una sincronización automática mientras la app esté abierta.
class SyncSettingsPage extends StatefulWidget {
  const SyncSettingsPage({super.key});

  @override
  State<SyncSettingsPage> createState() => _SyncSettingsPageState();
}

class _SyncSettingsPageState extends State<SyncSettingsPage> {
  final SyncConfigRepository _configRepository = sl<SyncConfigRepository>();
  final SyncEngine _syncEngine = sl<SyncEngine>();
  final AutoSyncScheduler _autoSyncScheduler = sl<AutoSyncScheduler>();
  final AppTrayService _trayService = sl<AppTrayService>();
  final LaunchAtStartupService _launchAtStartupService =
      sl<LaunchAtStartupService>();
  // Directo a `WindowPreferencesStore`, no a través de `AppTrayService` ni
  // `LaunchAtStartupService`: "iniciar minimizado" no es dueño de ninguno
  // de los dos (ver el plan de este slice sobre por qué la coordinación
  // entre ambos vive aquí, en la página, y no dentro de un servicio).
  final WindowPreferencesStore _windowPreferencesStore =
      sl<WindowPreferencesStore>();

  final _remotePathController = TextEditingController(text: '/');
  String? _localPath;

  /// Sentido de la sincronización (slice 13). Por defecto `download` -- una
  /// configuración anterior a este slice sigue comportándose igual hasta
  /// que el usuario elija otra cosa.
  SyncDirection _direction = SyncDirection.download;

  bool _syncing = false;
  String? _statusMessage;
  String? _startError;
  SyncResult? _lastResult;

  bool _autoSyncEnabled = false;
  int _autoSyncIntervalMinutes = _autoSyncIntervalOptions[1];
  ({DateTime at, String summary})? _lastAutoOutcome;

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

  /// Reflejo de `SyncEngine.onBusyChanged` -- true mientras CUALQUIER
  /// sincronización esté en curso, la haya arrancado el botón manual de
  /// esta página o un tick de `_autoSyncScheduler`. "Sincronizar ahora" se
  /// deshabilita también con esto para que un clic nunca pueda unirse en
  /// silencio a un tick automático de un par ya distinto al que se ve en
  /// pantalla (ver el porqué en el plan de este slice).
  bool _engineBusy = false;

  late final StreamSubscription<SyncResult> _autoResultSubscription;
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

    _loadSavedPair();
    _loadDirection();
    _loadAutoSyncSettings();
    _loadLaunchAtStartupSettings();
  }

  Future<void> _loadDirection() async {
    final direction = await _configRepository.readDirection();
    if (!mounted) return;
    setState(() => _direction = direction);
  }

  @override
  void dispose() {
    _autoResultSubscription.cancel();
    _autoStatusSubscription.cancel();
    _busySubscription.cancel();
    _remotePathController.dispose();
    super.dispose();
  }

  Future<void> _loadSavedPair() async {
    final saved = await _configRepository.read();
    if (saved == null || !mounted) return;
    setState(() {
      _remotePathController.text = saved.remotePath;
      _localPath = saved.localPath;
    });
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

  /// El invariante de "iniciar minimizado no puede quedar huérfano" se
  /// cierra en DOS sitios (ver el plan de este slice): aquí, al cargar la
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

  void _handleAutoResult(SyncResult result) {
    if (!mounted) return;
    // Mismo campo que ya pinta el bloque "Última sincronización" de más
    // abajo -- desde el punto de vista del usuario es el mismo concepto,
    // disparado a mano o solo, no hace falta una sección aparte.
    setState(() => _lastResult = result);
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

  Future<void> _pickLocalFolder() async {
    final path = await getDirectoryPath(confirmButtonText: 'Elegir carpeta');
    if (path == null || !mounted) return;
    setState(() => _localPath = path);
  }

  Future<void> _setDirection(SyncDirection direction) async {
    setState(() => _direction = direction);
    await _configRepository.saveDirection(direction);
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

  Future<void> _setMinimizeToTrayOnClose(bool value) async {
    setState(() => _minimizeToTrayOnClose = value);
    await _trayService.updateMinimizeToTrayOnClose(value);
  }

  /// A diferencia de los demás interruptores de esta página, envuelve la
  /// llamada en `try/catch` (ver el plan de este slice): a diferencia de
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

  Future<void> _syncNow() async {
    final remotePath = _remotePathController.text.trim();
    final localPath = _localPath;
    if (remotePath.isEmpty || localPath == null) {
      setState(() {
        _startError = 'Elige una carpeta remota y una carpeta local primero.';
      });
      return;
    }

    final pair = SyncPair(remotePath: remotePath, localPath: localPath);
    await _configRepository.save(pair);

    setState(() {
      _syncing = true;
      _startError = null;
      _lastResult = null;
      _statusMessage = 'Preparando...';
    });

    try {
      final result = await _syncEngine.syncNow(
        pair,
        direction: _direction,
        onStatus: (status) {
          if (!mounted) return;
          setState(() => _statusMessage = status);
        },
      );
      if (!mounted) return;
      setState(() {
        _syncing = false;
        _statusMessage = null;
        _lastResult = result;
      });
    } on ApiException catch (e) {
      // Fallo de arranque (p.ej. la carpeta remota configurada ya no
      // existe) -- distinto de los errores por archivo, que van dentro
      // de SyncResult.errors sin abortar el resto.
      if (!mounted) return;
      setState(() {
        _syncing = false;
        _statusMessage = null;
        _startError = e.message;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final syncDisabled = _syncing || _engineBusy;
    return Scaffold(
      appBar: AppBar(title: const Text('Sincronización')),
      // SingleChildScrollView, no Padding a secas: con auto-sync activado
      // (desplegable de intervalo visible) y el interruptor de bandeja del
      // slice 9 -- las dos secciones que se muestran u ocultan según
      // estado -- el contenido puede superar la altura de una ventana de
      // 720px real, y sin scroll eso desborda en vez de recortarse.
      body: SingleChildScrollView(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 520),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(
                  'Sincroniza una carpeta remota con una carpeta local. '
                  'Elige el sentido: "Descargar" solo trae del servidor, '
                  '"Subir" solo envía, "Ambos" reconcilia los dos lados. '
                  'Ningún modo borra nada todavía: un archivo que quites de '
                  'un lado se vuelve a traer del otro. En "Ambos", si un '
                  'archivo cambió en los dos sitios desde la última '
                  'sincronización, no se sobrescribe nada -- se deja una '
                  'copia "(conflicto ...)" al lado para que la revises. '
                  'Puedes activar la sincronización automática más abajo: '
                  'mientras la app esté abierta, se repetirá sola en el '
                  'intervalo elegido. Si cierras la app se detiene -- salvo '
                  'que también actives "Minimizar a la bandeja al cerrar". Y '
                  'si además activas "Arrancar con Windows", ni siquiera hace '
                  'falta abrir NexusCloud a mano tras reiniciar el equipo.',
                  style: Theme.of(context).textTheme.bodyMedium,
                ),
                const SizedBox(height: 24),
                Align(
                  alignment: Alignment.centerLeft,
                  child: SegmentedButton<SyncDirection>(
                    segments: [
                      for (final d in SyncDirection.values)
                        ButtonSegment(value: d, label: Text(d.label)),
                    ],
                    selected: {_direction},
                    onSelectionChanged: syncDisabled
                        ? null
                        : (selection) => _setDirection(selection.first),
                  ),
                ),
                const SizedBox(height: 16),
                TextField(
                  controller: _remotePathController,
                  enabled: !syncDisabled,
                  decoration: const InputDecoration(
                    labelText: 'Carpeta remota',
                    hintText: '/',
                  ),
                ),
                const SizedBox(height: 16),
                Row(
                  children: [
                    Expanded(
                      child: Text(
                        _localPath ?? 'Ninguna carpeta local elegida',
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                    const SizedBox(width: 12),
                    OutlinedButton(
                      onPressed: syncDisabled ? null : _pickLocalFolder,
                      child: const Text('Elegir carpeta local'),
                    ),
                  ],
                ),
                const SizedBox(height: 24),
                FilledButton(
                  onPressed: syncDisabled ? null : _syncNow,
                  child: _syncing
                      ? const SizedBox(
                          width: 20,
                          height: 20,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Text('Sincronizar ahora'),
                ),
                if (_statusMessage != null) ...[
                  const SizedBox(height: 16),
                  Text(
                    _statusMessage!,
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ],
                if (_startError != null) ...[
                  const SizedBox(height: 16),
                  Text(
                    _startError!,
                    style: TextStyle(
                      color: Theme.of(context).colorScheme.error,
                    ),
                  ),
                ],
                const SizedBox(height: 24),
                const Divider(),
                SwitchListTile(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('Sincronizar automáticamente'),
                  subtitle: const Text(
                    'Repite la sincronización sola mientras la app esté abierta.',
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
                if (_lastResult != null) ...[
                  const SizedBox(height: 24),
                  const Divider(),
                  const SizedBox(height: 8),
                  Text(
                    'Última sincronización: ${_lastResult!.finishedAt.toLocal()}\n'
                    '${_lastResult!.downloaded} descargados, '
                    '${_lastResult!.uploaded} subidos, '
                    '${_lastResult!.skipped} ya al día, '
                    '${_lastResult!.conflicts.length} conflictos, '
                    '${_lastResult!.errors.length} errores',
                  ),
                  if (_lastResult!.conflicts.isNotEmpty) ...[
                    const SizedBox(height: 8),
                    Text(
                      'Conflictos (se dejó una copia "(conflicto ...)" al '
                      'lado, revísala y funde los cambios a mano):',
                      style: Theme.of(context).textTheme.bodySmall,
                    ),
                    ConstrainedBox(
                      constraints: const BoxConstraints(maxHeight: 160),
                      child: ListView(
                        shrinkWrap: true,
                        children: [
                          for (final name in _lastResult!.conflicts)
                            Text('• $name'),
                        ],
                      ),
                    ),
                  ],
                  if (_lastResult!.errors.isNotEmpty) ...[
                    const SizedBox(height: 8),
                    ConstrainedBox(
                      constraints: const BoxConstraints(maxHeight: 200),
                      child: ListView(
                        shrinkWrap: true,
                        children: [
                          for (final error in _lastResult!.errors)
                            Text(
                              '• $error',
                              style: TextStyle(
                                color: Theme.of(context).colorScheme.error,
                              ),
                            ),
                        ],
                      ),
                    ),
                  ],
                ] else if (_lastAutoOutcome != null) ...[
                  // Sin ningún SyncResult todavía en esta sesión de la
                  // página (nunca se pulsó "Sincronizar ahora" ni corrió
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
}
