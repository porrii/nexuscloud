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

  /// Ajuste de `LocalChangeWatcherService` (slice 17): vigilar la carpeta
  /// local de cada par en `Subir`/`Ambos` y sincronizar sola poco después
  /// de un cambio, sin esperar al reloj de auto-sync. Un booleano suelto
  /// basta -- a diferencia de [AutoSyncSettings], no tiene más parámetros
  /// que el propio interruptor.
  Future<void> saveWatchLocalChanges(bool enabled);

  /// `false` si no hay nada guardado todavía -- mismo criterio
  /// "desactivado por defecto" que el resto de interruptores de esta app.
  Future<bool> readWatchLocalChanges();

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

  /// Umbral de la guarda anti-"borrado masivo" (ADR-013), configurable
  /// desde Ajustes (#23) en vez de fijo en el motor -- global para todos
  /// los pares, mismo criterio que [AutoSyncSettings] (ninguna de las
  /// tareas de esta serie pidió granularidad por par, y añadirla sin un
  /// caso de uso claro sería complejidad de UI sin beneficio real).
  Future<void> saveMaxAutoDeleteBatch(int value);

  /// `10` si no hay nada guardado todavía -- el mismo valor que ya traía
  /// `SyncEngine._maxAutoDeleteBatch` como constante antes de #23, para
  /// que una instalación existente no cambie de comportamiento sin que
  /// nadie lo haya pedido.
  Future<int> readMaxAutoDeleteBatch();
}
