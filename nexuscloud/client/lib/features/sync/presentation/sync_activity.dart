import 'dart:async';

import 'package:flutter/foundation.dart';

import '../domain/entities/pair_sync_outcome.dart';
import '../domain/repositories/sync_config_repository.dart';
import '../domain/services/auto_sync_scheduler.dart';
import '../domain/services/local_change_watcher_service.dart';
import '../domain/services/sync_engine.dart';

enum SyncHealth {
  /// Nada configurado todavía.
  idle,
  running,

  /// La última pasada terminó sin errores ni borrados pendientes.
  ok,

  /// Hay borrados pendientes de confirmar o conflictos que revisar.
  attention,
  error,
}

/// Estado de sincronización de toda la app, vivo mientras la app esté
/// abierta -- antes solo existía dentro de la página de Sincronización y se
/// perdía al salir de ella. Lo leen el indicador permanente de la barra
/// lateral y la propia página.
///
/// Escucha las mismas fuentes que la página escuchaba por su cuenta: el
/// motor (ocupado/libre), el reloj de auto-sync (resultados y mensajes) y
/// la vigilancia de cambios locales (mensajes). Un sync manual informa con
/// [beginManual] / [reportManualStatus] / [endManual].
class SyncActivity extends ChangeNotifier {
  SyncActivity({
    required SyncEngine syncEngine,
    required AutoSyncScheduler autoSyncScheduler,
    required LocalChangeWatcherService watcherService,
    required SyncConfigRepository configRepository,
  }) : _syncEngine = syncEngine,
       _scheduler = autoSyncScheduler,
       _watcher = watcherService,
       _configRepository = configRepository;

  final SyncEngine _syncEngine;
  final AutoSyncScheduler _scheduler;
  final LocalChangeWatcherService _watcher;
  final SyncConfigRepository _configRepository;

  final List<StreamSubscription<Object?>> _subscriptions = [];
  bool _initialized = false;

  bool _engineBusy = false;
  bool _manualRunning = false;
  String? _statusMessage;
  int _pairCount = 0;
  DateTime? _lastFinishedAt;
  ({DateTime at, String summary})? _lastAutoOutcome;

  /// Último resultado conocido por par (clave `SyncPair.stableKey`) --
  /// sobrevive a salir y volver a la página de Sincronización.
  final Map<String, PairSyncOutcome> _outcomesByKey = {};

  bool get isBusy => _engineBusy || _manualRunning;
  bool get manualRunning => _manualRunning;
  String? get statusMessage => _statusMessage;
  DateTime? get lastFinishedAt => _lastFinishedAt;
  ({DateTime at, String summary})? get lastAutoOutcome => _lastAutoOutcome;
  Map<String, PairSyncOutcome> get outcomesByKey =>
      Map.unmodifiable(_outcomesByKey);

  SyncHealth get health {
    if (isBusy) return SyncHealth.running;
    if (_pairCount == 0) return SyncHealth.idle;
    final outcomes = _outcomesByKey.values;
    if (outcomes.any(
      (o) => o.startupError != null || (o.result?.errors.isNotEmpty ?? false),
    )) {
      return SyncHealth.error;
    }
    if (outcomes.any(
      (o) =>
          (o.result?.pendingDeletes.isNotEmpty ?? false) ||
          (o.result?.conflicts.isNotEmpty ?? false),
    )) {
      return SyncHealth.attention;
    }
    return SyncHealth.ok;
  }

  /// Idempotente. Se llama al arrancar la app (tras `AutoSyncScheduler
  /// .start()`) para no perder resultados de ticks que lleguen antes de
  /// abrir ninguna pantalla.
  Future<void> init() async {
    if (_initialized) return;
    _initialized = true;
    _engineBusy = _syncEngine.isRunning;
    _subscriptions
      ..add(_syncEngine.onBusyChanged.listen(_handleBusy))
      ..add(_scheduler.onResult.listen(recordOutcomes))
      ..add(_scheduler.onStatus.listen(_handleBackgroundStatus))
      ..add(_watcher.onStatus.listen(_handleBackgroundStatus));
    await refreshConfig();
  }

  /// Relee el número de pares y el último resumen automático persistido.
  Future<void> refreshConfig() async {
    final pairs = await _configRepository.readPairs();
    final outcome = await _configRepository.readLastAutoSyncOutcome();
    _pairCount = pairs.length;
    _lastAutoOutcome = outcome;
    _lastFinishedAt ??= outcome?.at;
    final keys = {for (final c in pairs) c.pair.stableKey};
    _outcomesByKey.removeWhere((key, _) => !keys.contains(key));
    notifyListeners();
  }

  /// Combina [outcomes] con lo ya conocido -- sincronizar solo UN par no
  /// debe hacer desaparecer el último resultado de los demás.
  void recordOutcomes(List<PairSyncOutcome> outcomes) {
    for (final o in outcomes) {
      _outcomesByKey[o.pair.stableKey] = o;
    }
    _lastFinishedAt = DateTime.now();
    notifyListeners();
  }

  void forgetPair(String stableKey) {
    if (_outcomesByKey.remove(stableKey) != null) notifyListeners();
  }

  void beginManual() {
    _manualRunning = true;
    _statusMessage = 'Preparando...';
    notifyListeners();
  }

  void reportManualStatus(String status) {
    _statusMessage = status;
    notifyListeners();
  }

  void endManual() {
    _manualRunning = false;
    _statusMessage = null;
    notifyListeners();
  }

  void _handleBusy(bool busy) {
    _engineBusy = busy;
    if (!busy && !_manualRunning) {
      // Un tick automático (o una sincronización de la vigilancia local)
      // acaba de terminar -- limpia el mensaje de progreso para no dejarlo
      // colgado en pantalla.
      _statusMessage = null;
      _lastFinishedAt = DateTime.now();
    }
    notifyListeners();
  }

  void _handleBackgroundStatus(String status) {
    // Un sync manual en curso tiene prioridad: sus mensajes son los que el
    // usuario acaba de pedir ver.
    if (_manualRunning) return;
    _statusMessage = status;
    notifyListeners();
  }

  @override
  void dispose() {
    for (final s in _subscriptions) {
      s.cancel();
    }
    super.dispose();
  }
}
