import 'dart:async';

import '../../../../core/network/api_exception.dart';
import '../../../auth/domain/repositories/auth_repository.dart';
import '../entities/auto_sync_settings.dart';
import '../entities/sync_result.dart';
import '../repositories/sync_config_repository.dart';
import 'sync_engine.dart';

/// Dispara [SyncEngine.syncNow] en un intervalo fijo mientras la app esté
/// abierta (slice 8). Servicio de vida larga, un único singleton en DI --
/// misma forma que `SessionExpiryNotifier` (core/network): un
/// `StreamController.broadcast()` por cada evento que alguien pueda querer
/// observar, sin más estado expuesto.
///
/// Límite real y deliberado: no hay bandeja del sistema ni ejecución en
/// segundo plano en este cliente todavía, así que "automático" aquí
/// significa únicamente "mientras el proceso siga vivo" -- cerrar la
/// ventana detiene la sincronización automática igual que detendría
/// cualquier otro estado en memoria. Verdadero "sincroniza con la app
/// cerrada" requiere un slice futuro (bandeja del sistema).
class AutoSyncScheduler {
  AutoSyncScheduler({
    required SyncEngine syncEngine,
    required SyncConfigRepository configRepository,
    required AuthRepository authRepository,
    Timer Function(Duration duration, void Function(Timer timer) callback)?
        createTimer,
  })  : _syncEngine = syncEngine,
        _configRepository = configRepository,
        _authRepository = authRepository,
        // Inyectable únicamente para tests: un fake puede capturar la
        // duración y el callback sin depender de un `Timer` real ni de
        // `fake_async` (no es una dependencia de este proyecto). En
        // producción es literalmente `Timer.periodic`.
        _createTimer = createTimer ?? Timer.periodic;

  final SyncEngine _syncEngine;
  final SyncConfigRepository _configRepository;
  final AuthRepository _authRepository;
  final Timer Function(Duration duration, void Function(Timer timer) callback)
      _createTimer;

  Timer? _timer;

  final _resultController = StreamController<SyncResult>.broadcast();
  final _statusController = StreamController<String>.broadcast();

  /// Cada tick automático que de verdad llegó a ejecutar `syncNow` (no los
  /// que se saltaron por falta de sesión o de par configurado, ni los que
  /// fallaron en el arranque -- para esos últimos, ver
  /// `SyncConfigRepository.readLastAutoSyncOutcome`).
  Stream<SyncResult> get onResult => _resultController.stream;

  /// Mismo texto que ya emite `SyncEngine.syncNow` vía `onStatus`.
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

  /// Comprueba sesión y par configurado de nuevo en cada tick -- nunca
  /// valores cacheados al llamar a `start()` -- porque ambos pueden
  /// cambiar mientras el auto-sync sigue activado (el usuario cierra
  /// sesión sin desactivarlo, o cambia de carpeta configurada).
  ///
  /// Sin la comprobación de sesión, un tick que cae justo tras abrir la
  /// app pero antes de que el auto-login termine persistiría un "no se
  /// pudo conectar con el servidor" engañoso -- la causa real sería
  /// "todavía no hay sesión", no un problema de red.
  Future<void> _tick(Timer timer) async {
    if (_authRepository.currentUser == null) return;

    final pair = await _configRepository.read();
    if (pair == null) return;

    final direction = await _configRepository.readDirection();

    // La guarda de reentrancia de `SyncEngine.syncNow` protege esta
    // llamada automáticamente frente a solapamientos con un sync manual
    // o con otro tick que todavía siga en curso -- no hay nada especial
    // que hacer aquí para eso.
    try {
      // `confirmedDeletePaths` nunca se rellena aquí (ADR-013): un tick
      // automático sin nadie delante jamás confirma un borrado masivo -- si
      // `syncNow` devuelve `pendingDeletes`, se quedan pendientes hasta que
      // el usuario los revise a mano en Ajustes.
      final result = await _syncEngine.syncNow(
        pair,
        direction: direction,
        onStatus: _statusController.add,
      );
      final deleted = result.deletedRemote + result.deletedLocal;
      await _configRepository.saveLastAutoSyncOutcome(
        at: result.finishedAt,
        summary: '${result.downloaded} descargados, '
            '${result.uploaded} subidos, '
            '$deleted borrados, '
            '${result.pendingDeletes.length} borrados pendientes de confirmar, '
            '${result.conflicts.length} conflictos, '
            '${result.errors.length} errores',
      );
      _resultController.add(result);
    } on ApiException catch (e) {
      // Fallo de arranque (p.ej. la ruta remota configurada ya no
      // existe) -- no hay `SyncResult` que emitir, pero sí se deja
      // constancia en el último intento para que sea visible en Ajustes.
      await _configRepository.saveLastAutoSyncOutcome(
        at: DateTime.now().toUtc(),
        summary: 'Error: ${e.message}',
      );
    }
  }

  void dispose() {
    _timer?.cancel();
    _resultController.close();
    _statusController.close();
  }
}
