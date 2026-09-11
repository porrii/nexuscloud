import 'dart:io';

import '../entities/sync_pair.dart';

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
}
