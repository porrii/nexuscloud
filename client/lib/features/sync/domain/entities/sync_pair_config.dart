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
  const SyncPairConfig({
    required this.pair,
    required this.direction,
    this.autoSyncEnabled = true,
  });

  final SyncPair pair;
  final SyncDirection direction;

  /// Si este par participa en los ticks de `AutoSyncScheduler` (#24) --
  /// `true` por defecto para no cambiar el comportamiento de pares ya
  /// guardados (hoy TODOS los configurados participan). Nunca afecta a una
  /// sincronización disparada a mano (botón "Sincronizar todo ahora" o el
  /// de un par suelto en `SyncSettingsPage`), solo al reloj automático.
  final bool autoSyncEnabled;

  Map<String, dynamic> toJson() => {
        'remote_path': pair.remotePath,
        'local_path': pair.localPath,
        'direction': direction.storageValue,
        'auto_sync_enabled': autoSyncEnabled,
      };

  /// Un `direction` ausente o desconocido cae a [SyncDirection.download] --
  /// mismo criterio ya usado en `SyncDirection.fromStorage`. Un
  /// `auto_sync_enabled` ausente (par guardado antes de #24) cae a `true`,
  /// igual que el valor por defecto del constructor.
  factory SyncPairConfig.fromJson(Map<String, dynamic> json) => SyncPairConfig(
        pair: SyncPair(
          remotePath: json['remote_path'] as String,
          localPath: json['local_path'] as String,
        ),
        direction: SyncDirection.fromStorage(json['direction'] as String?),
        autoSyncEnabled: json['auto_sync_enabled'] as bool? ?? true,
      );

  @override
  List<Object?> get props => [pair, direction, autoSyncEnabled];
}
