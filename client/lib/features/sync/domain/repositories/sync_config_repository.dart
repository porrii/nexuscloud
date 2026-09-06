import '../entities/sync_pair.dart';

/// Persiste el único [SyncPair] configurado (slice A). Mismo patrón que
/// `ServerConfigStore` (core/storage): no es secreto, no necesita el
/// almacén seguro del SO.
abstract interface class SyncConfigRepository {
  Future<void> save(SyncPair pair);
  Future<SyncPair?> read();
  Future<void> clear();
}
