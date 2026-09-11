import 'dart:async';

import 'package:path/path.dart' as p;

import '../../../auth/domain/repositories/auth_repository.dart';
import '../entities/auto_sync_settings.dart';
import '../entities/pair_sync_outcome.dart';
import '../repositories/sync_config_repository.dart';
import 'multi_pair_sync_coordinator.dart';

/// Dispara [MultiPairSyncCoordinator.syncAllNow] en un intervalo fijo
/// mientras la app esté abierta (slice 8; slice 15 lo extiende de un único
/// par a la lista completa de pares configurados). Servicio de vida larga,
/// un único singleton en DI -- misma forma que `SessionExpiryNotifier`
/// (core/network): un `StreamController.broadcast()` por cada evento que
/// alguien pueda querer observar, sin más estado expuesto.
///
/// Límite real y deliberado: no hay bandeja del sistema ni ejecución en
/// segundo plano en este cliente todavía, así que "automático" aquí
/// significa únicamente "mientras el proceso siga vivo" -- cerrar la
/// ventana detiene la sincronización automática igual que detendría
/// cualquier otro estado en memoria. Verdadero "sincroniza con la app
/// cerrada" requiere un slice futuro (bandeja del sistema).
class AutoSyncScheduler {
  AutoSyncScheduler({
    required MultiPairSyncCoordinator coordinator,
    required SyncConfigRepository configRepository,
    required AuthRepository authRepository,
    Timer Function(Duration duration, void Function(Timer timer) callback)?
        createTimer,
  })  : _coordinator = coordinator,
        _configRepository = configRepository,
        _authRepository = authRepository,
        // Inyectable únicamente para tests: un fake puede capturar la
        // duración y el callback sin depender de un `Timer` real ni de
        // `fake_async` (no es una dependencia de este proyecto). En
        // producción es literalmente `Timer.periodic`.
        _createTimer = createTimer ?? Timer.periodic;

  final MultiPairSyncCoordinator _coordinator;
  final SyncConfigRepository _configRepository;
  final AuthRepository _authRepository;
  final Timer Function(Duration duration, void Function(Timer timer) callback)
      _createTimer;

  Timer? _timer;

  final _resultController = StreamController<List<PairSyncOutcome>>.broadcast();
  final _statusController = StreamController<String>.broadcast();

  /// Cada tick automático que de verdad llegó a ejecutar `syncAllNow` (no
  /// los que se saltaron por falta de sesión o de pares configurados). Un
  /// elemento por par -- ver `PairSyncOutcome`.
  Stream<List<PairSyncOutcome>> get onResult => _resultController.stream;

  /// Texto de estado de UN par a la vez, con el nombre de su carpeta local
  /// delante (`"<carpeta>: <estado>"`) para poder distinguirlos cuando hay
  /// varios -- mismo texto que ya emitía `SyncEngine.syncNow` vía
  /// `onStatus`, sin más.
  Stream<String> get onStatus => _statusController.stream;

  /// Se llama una vez en `main()`, tras `setupServiceLocator()` y antes de
  /// `runApp`: reactiva el auto-sync ya configurado en una sesión anterior
  /// sin que el usuario tenga que volver a entrar en Ajustes. Que el
  /// primer tick pueda caer antes de que el auto-login termine no es un
  /// problema -- `_tick` comprueba la sesión antes de hacer nada más.
  Future<void> start() async {
    final settings = await _configRepository.readAutoSync();
    _applySettings(settings);
  }

  /// Guarda los ajustes y rearma (o desarma) el temporizador para que el
  /// cambio se aplique al instante, sin esperar a reabrir la app.
  Future<void> updateSettings(AutoSyncSettings settings) async {
    await _configRepository.saveAutoSync(settings);
    _applySettings(settings);
  }

  void _applySettings(AutoSyncSettings settings) {
    _timer?.cancel();
    _timer = settings.enabled
        ? _createTimer(Duration(minutes: settings.intervalMinutes), _tick)
        : null;
  }

  /// Comprueba sesión y pares configurados de nuevo en cada tick -- nunca
  /// valores cacheados al llamar a `start()` -- porque ambos pueden
  /// cambiar mientras el auto-sync sigue activado (el usuario cierra
  /// sesión sin desactivarlo, o edita/quita/añade pares).
  ///
  /// Sin la comprobación de sesión, un tick que cae justo tras abrir la
  /// app pero antes de que el auto-login termine persistiría un "no se
  /// pudo conectar con el servidor" engañoso -- la causa real sería
  /// "todavía no hay sesión", no un problema de red.
  Future<void> _tick(Timer timer) async {
    if (_authRepository.currentUser == null) return;

    final pairs = await _configRepository.readPairs();
    if (pairs.isEmpty) return;

    // `confirmedDeletePathsByPair` nunca se rellena aquí (ADR-013): un tick
    // automático sin nadie delante jamás confirma un borrado masivo en
    // ningún par -- si `syncAllNow` devuelve `pendingDeletes` para alguno,
    // se quedan pendientes hasta que el usuario los revise a mano en
    // Ajustes. Un fallo de arranque en un par (`PairSyncOutcome.startupError`)
    // no impide que los demás se procesen -- ver `MultiPairSyncCoordinator`.
    final outcomes = await _coordinator.syncAllNow(
      pairs,
      onStatus: (pair, status) =>
          _statusController.add('${p.basename(pair.localPath)}: $status'),
    );

    await _configRepository.saveLastAutoSyncOutcome(
      at: DateTime.now().toUtc(),
      summary: outcomes.map(_summarizeOutcome).join(' | '),
    );
    _resultController.add(outcomes);
  }

  String _summarizeOutcome(PairSyncOutcome outcome) {
    final name = p.basename(outcome.pair.localPath);
    final error = outcome.startupError;
    if (error != null) return '$name: Error: $error';
    final result = outcome.result!;
    final deleted = result.deletedRemote + result.deletedLocal;
    return '$name: ${result.downloaded} descargados, '
        '${result.uploaded} subidos, $deleted borrados, '
        '${result.pendingDeletes.length} pendientes, '
        '${result.conflicts.length} conflictos, '
        '${result.errors.length} errores';
  }

  void dispose() {
    _timer?.cancel();
    _resultController.close();
    _statusController.close();
  }
}
