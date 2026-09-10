import '../entities/sync_pair.dart';
import '../entities/sync_state_entry.dart';

/// Persiste el *manifiesto de estado* de cada par: qué archivos estaban en
/// sync, y con qué tamaño/hash/fechas, tras la última reconciliación
/// correcta (ADR-012). Lo consume [SyncDirection.both] como base de su
/// clasificación de tres vías; los modos de un solo sentido también lo
/// mantienen al día para que cambiar a `both` más tarde no vea conflictos
/// espurios.
///
/// Clave de cada entrada: la ruta relativa a la raíz del par, siempre con
/// `/` (canónica, igual que las rutas remotas) -- nunca separadores del SO
/// local.
abstract interface class SyncStateStore {
  /// Mapa vacío si no hay manifiesto para [pair] todavía, o si el que hay
  /// está corrupto/ilegible -- nunca lanza (un manifiesto perdido degrada a
  /// "primera pasada", no rompe la sincronización).
  Future<Map<String, SyncStateEntry>> read(SyncPair pair);

  Future<void> write(SyncPair pair, Map<String, SyncStateEntry> entries);
}
