import 'package:equatable/equatable.dart';

import 'sync_pair.dart';
import 'sync_result.dart';

/// Resultado de sincronizar UN par dentro de una pasada de
/// `MultiPairSyncCoordinator.syncAllNow` (slice 15).
///
/// Exactamente uno de [result]/[startupError] está poblado: [result] si
/// `SyncEngine.syncNow` para este par terminó (con o sin errores por
/// archivo dentro de `SyncResult.errors`); [startupError] si el propio
/// arranque de la pasada para este par falló (p.ej. su carpeta remota ya
/// no existe, `ApiException`) -- un fallo de arranque en un par NUNCA
/// impide procesar los demás pares de la lista, a diferencia de
/// `SyncEngine.syncNow` con un solo par, donde antes de este slice ese
/// mismo fallo simplemente se propagaba a quien llamaba.
class PairSyncOutcome extends Equatable {
  const PairSyncOutcome({required this.pair, this.result, this.startupError});

  final SyncPair pair;
  final SyncResult? result;
  final String? startupError;

  @override
  List<Object?> get props => [pair, result, startupError];
}
