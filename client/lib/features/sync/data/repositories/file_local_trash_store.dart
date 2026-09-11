import 'dart:io';

import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

import '../../domain/entities/sync_pair.dart';
import '../../domain/repositories/local_trash_store.dart';

/// Mueve archivos a `getApplicationSupportDirectory()/local_trash/`, dentro
/// de una subcarpeta con la clave del par (`SyncPair.stableKey`), las
/// mismas subcarpetas relativas que tenía el archivo, y un nombre con sello
/// de tiempo delante -- en vez de borrarlos (ADR-013). Mismo patrón de
/// testabilidad que `FileSyncStateStore`: `baseDirectoryOverride`
/// para no depender de `path_provider` ni de un directorio real en tests.
class FileLocalTrashStore implements LocalTrashStore {
  FileLocalTrashStore({Directory? baseDirectoryOverride})
      : _baseDirectoryOverride = baseDirectoryOverride;

  final Directory? _baseDirectoryOverride;

  Future<Directory> _trashRoot(SyncPair pair) async {
    final base = _baseDirectoryOverride ?? await getApplicationSupportDirectory();
    return Directory(p.join(base.path, 'local_trash', pair.stableKey));
  }

  @override
  Future<void> moveToTrash({
    required SyncPair pair,
    required List<String> relativeSegments,
    required File file,
  }) async {
    final root = await _trashRoot(pair);
    final subDirs = relativeSegments.sublist(0, relativeSegments.length - 1);
    final destDir = Directory(p.joinAll([root.path, ...subDirs]));
    await destDir.create(recursive: true);

    // Sello único por microsegundo (más el nombre legible detrás) -- dos
    // borrados sucesivos del mismo relPath, incluso dentro de la misma
    // pasada, nunca se pisan (a diferencia de un timestamp a resolución de
    // segundo, que sí podría colisionar en un lote grande).
    final stamp = DateTime.now().toUtc().microsecondsSinceEpoch;
    final destPath = p.join(destDir.path, '${stamp}_${relativeSegments.last}');

    try {
      await file.rename(destPath);
    } on FileSystemException {
      // `rename` puede fallar si origen y destino están en volúmenes
      // distintos (la carpeta sincronizada y el directorio de datos de la
      // app no tienen por qué compartir unidad) -- copiar+borrar como
      // respaldo, no un caso de error real.
      await file.copy(destPath);
      await file.delete();
    }
  }
}
