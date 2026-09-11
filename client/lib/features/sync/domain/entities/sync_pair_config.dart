import 'package:equatable/equatable.dart';

import 'sync_direction.dart';
import 'sync_pair.dart';

/// Un par configurado dentro de la lista de sincronización (slice 15): la
/// carpeta remota/local en sí ([SyncPair]) más el sentido con el que ESTE
/// par se sincroniza ([SyncDirection]) -- cada par tiene el suyo, no hay un
/// sentido único global como en los slices 13-14.
///
/// `toJson`/`fromJson` viven aquí, no en un `data/models/` aparte: esto
/// nunca viaja por la API, solo se serializa a `shared_preferences`
/// (`SyncConfigRepositoryImpl`) -- mismo criterio que `SyncStateEntry`
/// (ADR-012).
class SyncPairConfig extends Equatable {
  const SyncPairConfig({required this.pair, required this.direction});

  final SyncPair pair;
  final SyncDirection direction;

  Map<String, dynamic> toJson() => {
        'remote_path': pair.remotePath,
        'local_path': pair.localPath,
        'direction': direction.storageValue,
      };

  /// Un `direction` ausente o desconocido cae a [SyncDirection.download] --
  /// mismo criterio ya usado en `SyncDirection.fromStorage`.
  factory SyncPairConfig.fromJson(Map<String, dynamic> json) => SyncPairConfig(
        pair: SyncPair(
          remotePath: json['remote_path'] as String,
          localPath: json['local_path'] as String,
        ),
        direction: SyncDirection.fromStorage(json['direction'] as String?),
      );

  @override
  List<Object?> get props => [pair, direction];
}
