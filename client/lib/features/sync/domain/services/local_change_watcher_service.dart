import 'dart:async';

import 'package:path/path.dart' as p;
import 'package:watcher/watcher.dart';

import '../../../auth/domain/repositories/auth_repository.dart';
import '../entities/sync_direction.dart';
import '../entities/sync_pair_config.dart';
import '../repositories/sync_config_repository.dart';
import 'multi_pair_sync_coordinator.dart';
import 'sync_engine.dart';

/// Cuánto esperar tras el ÚLTIMO evento de un par antes de sincronizarlo --
/// ni tan corto que dispare una pasada por cada escritura intermedia de un
/// solo guardado real (un editor puede generar varios eventos seguidos),
/// ni tan largo que deje de sentirse reactivo.
const _debounceWindow = Duration(seconds: 3);

/// Vigila la carpeta local de cada par en `Subir`/`Ambos` (slice 17) y
/// dispara `MultiPairSyncCoordinator.syncAllNow` para ESE par poco después
/// de un cambio, sin esperar al reloj de `AutoSyncScheduler` ni a un clic
/// manual. El lado remoto sigue sin vigilancia -- exigiría que el backend
/// notificara cambios (WebSocket/SSE), fuera de alcance de este slice.
///
/// Un par en `Descargar` nunca se vigila: en ese sentido ningún cambio
/// local se sube nunca (la propia UI ya lo explica -- "nada se borra
/// nunca, un archivo que falte se vuelve a traer del otro"), así que
/// reaccionar a sus cambios no tendría ningún efecto.
///
/// Deliberadamente NO usa `Timer.periodic`: cada evento de UN par reinicia
/// el temporizador de UN disparo de ESE par (debounce clásico, por par,
/// nunca global) -- mismo criterio de temporizador inyectable que
/// `AutoSyncScheduler`, para poder testear el debounce sin esperar tiempo
/// real ni tocar el filesystem de verdad.
class LocalChangeWatcherService {
  LocalChangeWatcherService({
    required MultiPairSyncCoordinator coordinator,
    required SyncEngine syncEngine,
    required SyncConfigRepository configRepository,
    required AuthRepository authRepository,
    Stream<WatchEvent> Function(String path)? watchDirectory,
    Timer Function(Duration duration, void Function() callback)?
        createDebounceTimer,
  })  : _coordinator = coordinator,
        _syncEngine = syncEngine,
        _configRepository = configRepository,
        _authRepository = authRepository,
        _watchDirectory = watchDirectory ?? _defaultWatchDirectory,
        _createDebounceTimer = createDebounceTimer ?? Timer.new;

  final MultiPairSyncCoordinator _coordinator;
  final SyncEngine _syncEngine;
  final SyncConfigRepository _configRepository;
  final AuthRepository _authRepository;
  final Stream<WatchEvent> Function(String path) _watchDirectory;
  final Timer Function(Duration duration, void Function() callback)
      _createDebounceTimer;

  static Stream<WatchEvent> _defaultWatchDirectory(String path) =>
      DirectoryWatcher(path).events;

  final _subscriptions = <String, StreamSubscription<WatchEvent>>{};
  final _debounceTimers = <String, Timer>{};
  final _watchedConfigs = <String, SyncPairConfig>{};

  bool _enabled = false;

  final _statusController = StreamController<String>.broadcast();

  /// Mismo texto `"<carpeta>: <estado>"` que ya emiten el sync manual y
  /// `AutoSyncScheduler.onStatus`.
  Stream<String> get onStatus => _statusController.stream;

  /// Se llama una vez en `main()`, tras `AutoSyncScheduler.start()`: lee el
  /// ajuste persistido y, si está activado, arma un watcher por cada par
  /// vigilable de la lista actual -- reactiva lo que ya estuviera
  /// configurado en una sesión anterior sin que el usuario toque nada.
  Future<void> start() async {
    _enabled = await _configRepository.readWatchLocalChanges();
    if (!_enabled) return;
    await updatePairs(await _configRepository.readPairs());
  }

  /// Activa/desactiva desde Ajustes -- persiste el ajuste y arma/desarma
  /// los watchers al instante, sin esperar a reabrir la app.
  Future<void> setEnabled(bool enabled) async {
    await _configRepository.saveWatchLocalChanges(enabled);
    _enabled = enabled;
    if (!enabled) {
      _cancelAll();
      return;
    }
    await updatePairs(await _configRepository.readPairs());
  }

  /// Reconcilia los watchers activos contra [pairs] -- se llama tras
  /// añadir/editar/quitar un par en `SyncSettingsPage`. Sin efecto si el
  /// ajuste está desactivado (nada que reconciliar).
  Future<void> updatePairs(List<SyncPairConfig> pairs) async {
    if (!_enabled) return;

    final wanted = {
      for (final cfg in pairs)
        if (_isWatchable(cfg.direction)) cfg.pair.stableKey: cfg,
    };

    for (final key in _watchedConfigs.keys.toList()) {
      final desired = wanted[key];
      final current = _watchedConfigs[key]!;
      if (desired == null || desired.pair.localPath != current.pair.localPath) {
        _stopWatching(key);
      }
    }
    for (final entry in wanted.entries) {
      if (_watchedConfigs.containsKey(entry.key)) {
        // La dirección pudo cambiar (p.ej. Subir->Ambos) sin que cambiara
        // la ruta local -- el watcher del filesystem no depende de la
        // dirección, basta con refrescar qué config usará el próximo
        // disparo.
        _watchedConfigs[entry.key] = entry.value;
      } else {
        _startWatching(entry.value);
      }
    }
  }

  bool _isWatchable(SyncDirection direction) =>
      direction == SyncDirection.upload || direction == SyncDirection.both;

  void _startWatching(SyncPairConfig config) {
    final key = config.pair.stableKey;
    _watchedConfigs[key] = config;
    _subscriptions[key] = _watchDirectory(config.pair.localPath).listen(
      (_) => _onEvent(key),
      onError: (Object _) {
        // DirectoryWatcher ya se reestablece solo en Windows ante un fallo
        // del SDK (ver su documentación); aquí solo hace falta no dejar
        // que un error tire el servicio entero ni al resto de watchers.
      },
    );
  }

  void _stopWatching(String key) {
    _subscriptions.remove(key)?.cancel();
    _debounceTimers.remove(key)?.cancel();
    _watchedConfigs.remove(key);
  }

  void _cancelAll() {
    for (final key in _watchedConfigs.keys.toList()) {
      _stopWatching(key);
    }
  }

  void _onEvent(String key) {
    _debounceTimers[key]?.cancel();
    _debounceTimers[key] = _createDebounceTimer(_debounceWindow, () => _fire(key));
  }

  Future<void> _fire(String key) async {
    final config = _watchedConfigs[key];
    // Se quitó (o dejó de ser vigilable) mientras esperaba el debounce.
    if (config == null) return;
    if (_authRepository.currentUser == null) return;
    // La pasada en curso (disparada por el reloj, un clic, o un evento
    // anterior de este mismo watcher) ya va a dejar el árbol al día -- no
    // hace falta apilar otra detrás.
    if (_syncEngine.isPairRunning(config.pair)) return;

    await _coordinator.syncAllNow(
      [config],
      onStatus: (pair, status) =>
          _statusController.add('${p.basename(pair.localPath)}: $status'),
    );
  }

  void dispose() {
    _cancelAll();
    _statusController.close();
  }
}
