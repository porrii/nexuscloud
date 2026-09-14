import 'dart:io';

import '../entities/local_trash_entry.dart';
import '../entities/sync_pair.dart';

/// Se lanza desde [LocalTrashStore.restore] cuando ya existe un archivo en
/// el destino -- restaurar NUNCA sobrescribe en silencio (mismo espíritu
/// que el resto de ADR-013). [message] está pensado para mostrarse tal
/// cual en la UI.
class LocalTrashRestoreConflict implements Exception {
  const LocalTrashRestoreConflict(this.message);

  final String message;

  @override
  String toString() => message;
}

/// Papelera local para archivos borrados en remoto y propagados a local
/// (ADR-013): en vez de `File.delete()` directo -- que sería un borrado
/// definitivo sin red de seguridad -- el archivo se MUEVE aquí, recuperable
/// a mano. Mismo espíritu que la papelera del servidor (ADR-006) y que las
/// *conflict copies* del slice 13: nunca destruir en silencio. Vive en el
/// directorio de datos de la app, igual que el manifiesto de estado de
/// `FileSyncStateStore` (ADR-012) -- nunca dentro de la carpeta
/// sincronizada.
abstract interface class LocalTrashStore {
  /// Mueve [file] (parte de [pair], en la ruta relativa [relativeSegments])
  /// a la papelera local. Nunca lanza por "ya existe un destino igual" --
  /// cada llamada genera un nombre único (sello de tiempo), así que dos
  /// borrados sucesivos del mismo `relPath` nunca se pisan.
  Future<void> moveToTrash({
    required SyncPair pair,
    required List<String> relativeSegments,
    required File file,
  });

  /// Todo lo que hay en la papelera local, de cualquier par, para que la UI
  /// (slice 16) pueda listarlo. Una entrada que no se pueda interpretar (o
  /// un error de E/S puntual al leerla) se salta en vez de abortar el
  /// listado completo -- nunca debería pasar en uso normal, pero no hay
  /// motivo para que un archivo raro esconda el resto de la papelera.
  Future<List<LocalTrashEntry>> listAll();

  /// Mueve [entry] de vuelta a `destinationLocalPath/<relativeSegments>`,
  /// creando las carpetas intermedias que falten. Lanza
  /// [LocalTrashRestoreConflict] -- sin tocar ni origen ni destino -- si ya
  /// existe un archivo ahí.
  Future<void> restore({
    required LocalTrashEntry entry,
    required String destinationLocalPath,
  });

  /// Borra [entry] de la papelera para siempre. Irreversible.
  Future<void> deleteForever(LocalTrashEntry entry);
}
