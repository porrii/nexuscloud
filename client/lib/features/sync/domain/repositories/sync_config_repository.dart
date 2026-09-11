import '../entities/auto_sync_settings.dart';
import '../entities/sync_pair_config.dart';

/// Persiste la lista de pares configurados (slice 15 -- antes, slices A-14,
/// un único par), los ajustes de sincronización automática (slice 8, siguen
/// siendo globales: se aplican a todos los pares) y el último resultado
/// automático. Mismo patrón que `ServerConfigStore` (core/storage): no es
/// secreto, no necesita el almacén seguro del SO.
abstract interface class SyncConfigRepository {
  Future<void> savePairs(List<SyncPairConfig> pairs);

  /// Lista vacía si no hay nada guardado todavía. Si la app tenía un único
  /// par configurado de una versión anterior al slice 15 (claves
  /// `remote_path`/`local_path`, con o sin `direction`), la primera lectura
  /// lo migra automáticamente a una lista de un elemento -- transparente
  /// para el usuario, no hace falta reconfigurar nada.
  Future<List<SyncPairConfig>> readPairs();

  Future<void> saveAutoSync(AutoSyncSettings settings);

  /// [AutoSyncSettings.disabled] si no hay nada guardado todavía.
  Future<AutoSyncSettings> readAutoSync();

  /// Se persiste tras cada intento automático (con al menos un par
  /// configurado) para que sea visible en Ajustes aunque la página no
  /// estuviera abierta en ese momento. Se guarda como resumen de texto, no
  /// la lista de `PairSyncOutcome` completa -- este cliente no tiene una
  /// capa de (de)serialización JSON genérica para `shared_preferences`
  /// (los sitios que sí serializan, como los propios pares, lo hacen cada
  /// uno con su `toJson`/`fromJson`, sin una capa compartida) y no vale la
  /// pena introducirla solo para esto.
  Future<void> saveLastAutoSyncOutcome({
    required DateTime at,
    required String summary,
  });

  Future<({DateTime at, String summary})?> readLastAutoSyncOutcome();
}
