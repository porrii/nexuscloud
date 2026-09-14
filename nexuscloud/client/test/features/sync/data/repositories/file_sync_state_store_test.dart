import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/features/sync/data/repositories/file_sync_state_store.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_pair.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_state_entry.dart';
import 'package:path/path.dart' as p;

void main() {
  late Directory tempDir;
  late FileSyncStateStore store;

  const pair = SyncPair(remotePath: '/Documentos', localPath: r'C:\local');

  setUp(() {
    tempDir = Directory.systemTemp.createTempSync('nexuscloud_manifest_');
    store = FileSyncStateStore(baseDirectoryOverride: tempDir);
  });

  tearDown(() {
    if (tempDir.existsSync()) tempDir.deleteSync(recursive: true);
  });

  SyncStateEntry entry(int size) => SyncStateEntry(
        remoteSizeBytes: size,
        localSizeBytes: size,
        sha256: 'sha-$size',
        remoteUpdatedAt: DateTime.utc(2026, 5, 1, 12),
        localModifiedAt: DateTime.utc(2026, 5, 1, 12, 0, 1),
      );

  test('read devuelve un mapa vacío si no hay manifiesto todavía', () async {
    expect(await store.read(pair), isEmpty);
  });

  test('write y read hacen round-trip de las entradas', () async {
    final entries = {
      'a.txt': entry(10),
      'sub/b.bin': entry(2048),
    };

    await store.write(pair, entries);
    final read = await store.read(pair);

    expect(read, equals(entries));
  });

  test('cada par usa un fichero distinto', () async {
    const otherPair = SyncPair(remotePath: '/Fotos', localPath: r'C:\otra');

    await store.write(pair, {'a.txt': entry(1)});
    await store.write(otherPair, {'z.txt': entry(2)});

    expect((await store.read(pair)).keys, ['a.txt']);
    expect((await store.read(otherPair)).keys, ['z.txt']);
  });

  test('un manifiesto corrupto se lee como vacío en vez de lanzar', () async {
    // Escribe algo válido para conocer la ruta del fichero, luego lo
    // corrompe.
    await store.write(pair, {'a.txt': entry(1)});
    final stateDir = Directory(p.join(tempDir.path, 'sync_state'));
    final file = stateDir.listSync().whereType<File>().single;
    await file.writeAsString('{ esto no es json valido ');

    expect(await store.read(pair), isEmpty);
  });

  test('write es atómico: no deja ningún .tmp al terminar', () async {
    await store.write(pair, {'a.txt': entry(1)});
    final stateDir = Directory(p.join(tempDir.path, 'sync_state'));
    final leftovers =
        stateDir.listSync().whereType<File>().where((f) => f.path.endsWith('.tmp'));
    expect(leftovers, isEmpty);
  });
}
