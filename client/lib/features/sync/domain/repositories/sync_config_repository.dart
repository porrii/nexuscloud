import '../entities/auto_sync_settings.dart';
import '../entities/sync_pair.dart';

/// Persiste el único [SyncPair] configurado (slice A), y desde el slice 8
/// también los ajustes de sincronización automática. Mismo patrón que
/// `ServerConfigStore` (core/storage): no es secreto, no necesita el
/// almacén seguro del SO.
abstract interface class SyncConfigRepository {
  Future<void> save(SyncPair pair);
  Future<SyncPair?> read();
  Future<void> clear();

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
