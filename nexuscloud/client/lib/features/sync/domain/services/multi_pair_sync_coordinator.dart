import '../../../../core/network/api_exception.dart';
import '../entities/pair_sync_outcome.dart';
import '../entities/sync_pair.dart';
import '../entities/sync_pair_config.dart';
import 'sync_engine.dart';

/// Orquesta `SyncEngine.syncNow` sobre VARIOS pares (slice 15, ADR-014).
/// Deliberadamente en un fichero propio, no como un método más de
/// `SyncEngine`: ese fichero (ADR-011 slices 3/8, ADR-012 slice 13, ADR-013
/// slice 14) ya rozaba las 800 líneas de golden-principles.md #5 antes de
/// este slice, y "sincronizar un par" y "recorrer varios pares" son
/// responsabilidades distintas -- el motor no necesita saber que existen
/// varios.
///
/// Recorre la lista **secuencialmente** (mismo criterio que el recorrido
/// remoto dentro de un par, ADR-011 dec. 5: nada que paralelizar a la
/// escala de uso personal de §160) y atrapa la `ApiException` de arranque
/// de CADA par por separado -- a diferencia de `SyncEngine.syncNow` con un
/// solo par (donde ese fallo simplemente se propaga a quien llama), aquí un
/// par con la carpeta remota ya inexistente no debe impedir que los demás
/// se sincronicen.
class MultiPairSyncCoordinator {
  MultiPairSyncCoordinator({required SyncEngine syncEngine})
      : _syncEngine = syncEngine;

  final SyncEngine _syncEngine;

  /// [confirmedDeletePathsByPair] enruta cada conjunto de claves confirmadas
  /// (ADR-013) SOLO al par cuya `pair.stableKey` coincide -- confirmar un
  /// lote de borrados de un par nunca afecta a otro.
  Future<List<PairSyncOutcome>> syncAllNow(
    List<SyncPairConfig> pairs, {
    void Function(SyncPair pair, String status)? onStatus,
    Map<String, Set<String>>? confirmedDeletePathsByPair,
  }) async {
    final outcomes = <PairSyncOutcome>[];
    for (final config in pairs) {
      try {
        final result = await _syncEngine.syncNow(
          config.pair,
          direction: config.direction,
          confirmedDeletePaths: confirmedDeletePathsByPair?[config.pair.stableKey],
          onStatus: onStatus == null
              ? null
              : (status) => onStatus(config.pair, status),
        );
        outcomes.add(PairSyncOutcome(pair: config.pair, result: result));
      } on ApiException catch (e) {
        outcomes.add(PairSyncOutcome(pair: config.pair, startupError: e.message));
      }
    }
    return outcomes;
  }
}
