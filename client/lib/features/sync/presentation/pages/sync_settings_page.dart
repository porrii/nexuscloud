import 'dart:async';

import 'package:file_selector/file_selector.dart';
import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/network/api_exception.dart';
import '../../../../core/window/app_tray_service.dart';
import '../../domain/entities/auto_sync_settings.dart';
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

  final _remotePathController = TextEditingController(text: '/');
  String? _localPath;

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
    _loadAutoSyncSettings();
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
                  'Sincroniza una carpeta remota hacia una carpeta local. '
                  'Por ahora es de un solo sentido (servidor → local) -- no '
                  'borra ni sube nada, solo trae lo nuevo o cambiado. Puedes '
                  'activar la sincronización automática más abajo: mientras '
                  'la app esté abierta, se repetirá sola en el intervalo '
                  'elegido. Si cierras la app se detiene -- salvo que también '
                  'actives "Minimizar a la bandeja al cerrar" más abajo.',
                  style: Theme.of(context).textTheme.bodyMedium,
                ),
                const SizedBox(height: 24),
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
                if (_lastResult != null) ...[
                  const SizedBox(height: 24),
                  const Divider(),
                  const SizedBox(height: 8),
                  Text(
                    'Última sincronización: ${_lastResult!.finishedAt.toLocal()}\n'
                    '${_lastResult!.downloaded} descargados, '
                    '${_lastResult!.skipped} ya al día, '
                    '${_lastResult!.errors.length} errores',
                  ),
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
