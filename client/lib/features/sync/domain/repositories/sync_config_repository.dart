import '../entities/auto_sync_settings.dart';
import '../entities/sync_direction.dart';
import '../entities/sync_pair.dart';

/// Persiste el único [SyncPair] configurado (slice A), desde el slice 8 los
/// ajustes de sincronización automática, y desde el slice 13 la dirección
/// de sincronización. Mismo patrón que `ServerConfigStore` (core/storage):
/// no es secreto, no necesita el almacén seguro del SO.
abstract interface class SyncConfigRepository {
  Future<void> save(SyncPair pair);
  Future<SyncPair?> read();
  Future<void> clear();

  Future<void> saveDirection(SyncDirection direction);

  /// [SyncDirection.download] si no hay nada guardado todavía -- el
  /// comportamiento anterior al slice 13, para no cambiar en silencio lo
  /// que hace una configuración ya existente.
  Future<SyncDirection> readDirection();

  Future<void> saveAutoSync(AutoSyncSettings settings);

  /// [AutoSyncSettings.disabled] si no hay nada guardado todavía.
  Future<AutoSyncSettings> readAutoSync();

  /// Se persiste tras cada intento automático (éxito o fallo) para que
  /// sea visible en Ajustes aunque la página no estuviera abierta en ese
  /// momento. Se guarda como resumen de texto, no el `SyncResult`
  /// completo -- este cliente no tiene una capa de (de)serialización JSON
  /// para `shared_preferences` en ningún sitio, y no vale la pena
  /// introducirla solo para esto.
  Future<void> saveLastAutoSyncOutcome({
    required DateTime at,
    required String summary,
  });

  Future<({DateTime at, String summary})?> readLastAutoSyncOutcome();
}
