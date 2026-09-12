import 'dart:io';

import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

import '../../domain/entities/local_trash_entry.dart';
import '../../domain/entities/sync_pair.dart';
import '../../domain/repositories/local_trash_store.dart';

/// `moveToTrash` escribe `<microsegundos>_<nombre original>` -- este patrón
/// separa las dos partes al listar. Un nombre que no encaje (nunca debería
/// pasar con archivos que esta clase escribió, pero un usuario podría dejar
/// algo raro a mano dentro del directorio de datos) se descarta en vez de
/// romper el listado completo.
final _stampedNamePattern = RegExp(r'^(\d+)_(.+)$');

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

  Future<Directory> _trashBase() async =>
      Directory(p.join(
        (_baseDirectoryOverride ?? await getApplicationSupportDirectory()).path,
        'local_trash',
      ));

  @override
  Future<List<LocalTrashEntry>> listAll() async {
    final root = await _trashBase();
    if (!root.existsSync()) return [];

    final entries = <LocalTrashEntry>[];
    await for (final entity in root.list(recursive: true, followLinks: false)) {
      if (entity is! File) continue;
      final entry = _tryParseEntry(root, entity);
      if (entry != null) entries.add(entry);
    }
    return entries;
  }

  /// `null` -> se descarta silenciosamente (ver la nota de
  /// `_stampedNamePattern`); nunca lanza.
  LocalTrashEntry? _tryParseEntry(Directory root, File file) {
    try {
      final relPath = p.relative(file.path, from: root.path);
      final segments = p.split(relPath);
      // Hace falta al menos <pairKey>/<archivo> -- cualquier cosa suelta
      // directamente bajo la raíz de la papelera no encaja en el formato.
      if (segments.length < 2) return null;

      final pairKey = segments.first;
      final match = _stampedNamePattern.firstMatch(segments.last);
      if (match == null) return null;
      final stampMicros = int.tryParse(match.group(1)!);
      if (stampMicros == null) return null;
      final originalName = match.group(2)!;

      final relativeSegments = [
        ...segments.sublist(1, segments.length - 1),
        originalName,
      ];
      final sizeBytes = file.lengthSync();

      return LocalTrashEntry(
        pairKey: pairKey,
        relativeSegments: relativeSegments,
        deletedAt: DateTime.fromMicrosecondsSinceEpoch(stampMicros, isUtc: true),
        sizeBytes: sizeBytes,
        absolutePath: file.path,
      );
    } on FileSystemException {
      return null;
    }
  }

  @override
  Future<void> restore({
    required LocalTrashEntry entry,
    required String destinationLocalPath,
  }) async {
    final destPath = p.joinAll([destinationLocalPath, ...entry.relativeSegments]);
    final destFile = File(destPath);
    if (await destFile.exists()) {
      throw LocalTrashRestoreConflict(
        'Ya existe un archivo en "${entry.displayPath}" -- muévelo o '
        'renómbralo antes de restaurar.',
      );
    }

    await destFile.parent.create(recursive: true);
    final source = File(entry.absolutePath);
    try {
      await source.rename(destPath);
    } on FileSystemException {
      // Mismo motivo que en `moveToTrash`: origen y destino pueden estar en
      // volúmenes distintos.
      await source.copy(destPath);
      await source.delete();
    }
  }

  @override
  Future<void> deleteForever(LocalTrashEntry entry) async {
    await File(entry.absolutePath).delete();
  }
}
