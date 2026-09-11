import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/features/sync/data/repositories/file_local_trash_store.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_pair.dart';
import 'package:path/path.dart' as p;

void main() {
  late Directory sourceDir;
  late Directory trashBaseDir;
  late FileLocalTrashStore store;

  const pair = SyncPair(remotePath: '/Documentos', localPath: r'C:\local');

  setUp(() {
    sourceDir = Directory.systemTemp.createTempSync('nexuscloud_trash_src_');
    trashBaseDir = Directory.systemTemp.createTempSync('nexuscloud_trash_base_');
    store = FileLocalTrashStore(baseDirectoryOverride: trashBaseDir);
  });

  tearDown(() {
    if (sourceDir.existsSync()) sourceDir.deleteSync(recursive: true);
    if (trashBaseDir.existsSync()) trashBaseDir.deleteSync(recursive: true);
  });

  test('mueve el archivo a la papelera y conserva el contenido', () async {
    final file = File(p.join(sourceDir.path, 'a.txt'));
    await file.writeAsBytes([1, 2, 3]);

    await store.moveToTrash(pair: pair, relativeSegments: const ['a.txt'], file: file);

    expect(file.existsSync(), isFalse);
    final trashDir = Directory(p.join(trashBaseDir.path, 'local_trash', pair.stableKey));
    final trashed = trashDir.listSync().whereType<File>().toList();
    expect(trashed, hasLength(1));
    expect(trashed.single.path, endsWith('_a.txt'));
    expect(await trashed.single.readAsBytes(), [1, 2, 3]);
  });

  test('conserva las subcarpetas relativas dentro de la papelera', () async {
    final subDir = Directory(p.join(sourceDir.path, 'carpeta'));
    await subDir.create();
    final file = File(p.join(subDir.path, 'anidado.txt'));
    await file.writeAsString('contenido');

    await store.moveToTrash(
      pair: pair,
      relativeSegments: const ['carpeta', 'anidado.txt'],
      file: file,
    );

    final expectedDir = Directory(
      p.join(trashBaseDir.path, 'local_trash', pair.stableKey, 'carpeta'),
    );
    final trashed = expectedDir.listSync().whereType<File>().toList();
    expect(trashed, hasLength(1));
    expect(trashed.single.path, endsWith('_anidado.txt'));
  });

  test('dos borrados sucesivos del mismo relPath no se pisan', () async {
    final file1 = File(p.join(sourceDir.path, 'a.txt'));
    await file1.writeAsString('primera versión');
    await store.moveToTrash(pair: pair, relativeSegments: const ['a.txt'], file: file1);

    // Recrea el mismo relPath local y lo borra otra vez.
    final file2 = File(p.join(sourceDir.path, 'a.txt'));
    await file2.writeAsString('segunda versión');
    await store.moveToTrash(pair: pair, relativeSegments: const ['a.txt'], file: file2);

    final trashDir = Directory(p.join(trashBaseDir.path, 'local_trash', pair.stableKey));
    final trashed = trashDir.listSync().whereType<File>().toList();
    expect(trashed, hasLength(2));
    final contents = await Future.wait(trashed.map((f) => f.readAsString()));
    expect(contents, containsAll(['primera versión', 'segunda versión']));
  });

  test('cada par usa una subcarpeta distinta dentro de la papelera', () async {
    const otherPair = SyncPair(remotePath: '/Fotos', localPath: r'C:\otra');
    final file = File(p.join(sourceDir.path, 'a.txt'));
    await file.writeAsString('x');

    await store.moveToTrash(pair: otherPair, relativeSegments: const ['a.txt'], file: file);

    final wrongDir = Directory(p.join(trashBaseDir.path, 'local_trash', pair.stableKey));
    final rightDir =
        Directory(p.join(trashBaseDir.path, 'local_trash', otherPair.stableKey));
    expect(wrongDir.existsSync(), isFalse);
    expect(rightDir.listSync().whereType<File>(), hasLength(1));
  });
}
