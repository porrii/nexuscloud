import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/features/sync/data/repositories/file_local_trash_store.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_pair.dart';
import 'package:nexuscloud_client/features/sync/domain/repositories/local_trash_store.dart';
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

  group('listAll/restore/deleteForever (slice 16)', () {
    test('listAll() vacío si no hay nada en la papelera', () async {
      expect(await store.listAll(), isEmpty);
    });

    test(
      'listAll() recoge entradas de varios pares, con subcarpetas anidadas, con datos correctos',
      () async {
        const otherPair = SyncPair(remotePath: '/Fotos', localPath: r'C:\otra');
        final flatFile = File(p.join(sourceDir.path, 'suelto.txt'));
        await flatFile.writeAsBytes([1, 2, 3, 4]);
        await store.moveToTrash(pair: pair, relativeSegments: const ['suelto.txt'], file: flatFile);

        final nestedDir = Directory(p.join(sourceDir.path, 'carpeta'));
        await nestedDir.create();
        final nestedFile = File(p.join(nestedDir.path, 'anidado.txt'));
        await nestedFile.writeAsString('contenido anidado');
        await store.moveToTrash(
          pair: otherPair,
          relativeSegments: const ['carpeta', 'anidado.txt'],
          file: nestedFile,
        );

        final before = DateTime.now().toUtc().subtract(const Duration(seconds: 5));
        final entries = await store.listAll();
        final after = DateTime.now().toUtc().add(const Duration(seconds: 5));

        expect(entries, hasLength(2));
        final flatEntry = entries.firstWhere((e) => e.pairKey == pair.stableKey);
        expect(flatEntry.relativeSegments, ['suelto.txt']);
        expect(flatEntry.sizeBytes, 4);
        expect(flatEntry.deletedAt.isAfter(before) && flatEntry.deletedAt.isBefore(after), isTrue);
        expect(File(flatEntry.absolutePath).existsSync(), isTrue);

        final nestedEntry = entries.firstWhere((e) => e.pairKey == otherPair.stableKey);
        expect(nestedEntry.relativeSegments, ['carpeta', 'anidado.txt']);
        expect(nestedEntry.displayPath, 'carpeta/anidado.txt');
      },
    );

    test(
      'listAll() se salta una entrada con nombre que no encaja en el patrón, sin romper el resto',
      () async {
        final file = File(p.join(sourceDir.path, 'a.txt'));
        await file.writeAsString('normal');
        await store.moveToTrash(pair: pair, relativeSegments: const ['a.txt'], file: file);

        // Fichero puesto a mano, sin pasar por moveToTrash -> sin el sello
        // de tiempo delante, no encaja en `sello_nombre`.
        final rareDir = Directory(p.join(trashBaseDir.path, 'local_trash', pair.stableKey));
        await File(p.join(rareDir.path, 'raro_sin_sello.txt')).writeAsString('raro');

        final entries = await store.listAll();

        expect(entries, hasLength(1));
        expect(entries.single.relativeSegments, ['a.txt']);
      },
    );

    test(
      'restore() mueve el archivo de vuelta respetando relativeSegments y conserva el contenido',
      () async {
        final nestedDir = Directory(p.join(sourceDir.path, 'carpeta'));
        await nestedDir.create();
        final file = File(p.join(nestedDir.path, 'doc.txt'));
        await file.writeAsString('contenido original');
        await store.moveToTrash(
          pair: pair,
          relativeSegments: const ['carpeta', 'doc.txt'],
          file: file,
        );
        final entry = (await store.listAll()).single;

        final destinationDir = Directory.systemTemp.createTempSync('nexuscloud_restore_dest_');
        addTearDown(() {
          if (destinationDir.existsSync()) destinationDir.deleteSync(recursive: true);
        });

        await store.restore(entry: entry, destinationLocalPath: destinationDir.path);

        final restored = File(p.join(destinationDir.path, 'carpeta', 'doc.txt'));
        expect(restored.existsSync(), isTrue);
        expect(await restored.readAsString(), 'contenido original');
        expect(File(entry.absolutePath).existsSync(), isFalse);
      },
    );

    test(
      'restore() lanza LocalTrashRestoreConflict si el destino ya existe, sin tocar origen ni destino',
      () async {
        final file = File(p.join(sourceDir.path, 'a.txt'));
        await file.writeAsString('version en la papelera');
        await store.moveToTrash(pair: pair, relativeSegments: const ['a.txt'], file: file);
        final entry = (await store.listAll()).single;

        final destinationDir = Directory.systemTemp.createTempSync('nexuscloud_restore_dest_');
        addTearDown(() {
          if (destinationDir.existsSync()) destinationDir.deleteSync(recursive: true);
        });
        final conflicting = File(p.join(destinationDir.path, 'a.txt'));
        await conflicting.writeAsString('version que ya estaba en destino');

        await expectLater(
          () => store.restore(entry: entry, destinationLocalPath: destinationDir.path),
          throwsA(isA<LocalTrashRestoreConflict>()),
        );

        expect(File(entry.absolutePath).existsSync(), isTrue, reason: 'el origen no debe tocarse');
        expect(await conflicting.readAsString(), 'version que ya estaba en destino');
      },
    );

    test('deleteForever() borra el archivo para siempre sin afectar a otras entradas', () async {
      final fileA = File(p.join(sourceDir.path, 'a.txt'));
      await fileA.writeAsString('a');
      await store.moveToTrash(pair: pair, relativeSegments: const ['a.txt'], file: fileA);

      final fileB = File(p.join(sourceDir.path, 'b.txt'));
      await fileB.writeAsString('b');
      await store.moveToTrash(pair: pair, relativeSegments: const ['b.txt'], file: fileB);

      final entries = await store.listAll();
      final entryToDelete = entries.firstWhere((e) => e.relativeSegments.single == 'a.txt');
      final entryToKeep = entries.firstWhere((e) => e.relativeSegments.single == 'b.txt');

      await store.deleteForever(entryToDelete);

      expect(File(entryToDelete.absolutePath).existsSync(), isFalse);
      expect(File(entryToKeep.absolutePath).existsSync(), isTrue);
      final remaining = await store.listAll();
      expect(remaining, hasLength(1));
      expect(remaining.single.relativeSegments, ['b.txt']);
    });
  });
}
