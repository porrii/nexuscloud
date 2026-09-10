import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/network/api_exception.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_entry.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_listing.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_entry.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_version.dart';
import 'package:nexuscloud_client/features/files/domain/repositories/files_repository.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_direction.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_pair.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_state_entry.dart';
import 'package:nexuscloud_client/features/sync/domain/repositories/sync_state_store.dart';
import 'package:nexuscloud_client/features/sync/domain/services/sync_engine.dart';

/// Fake con un árbol remoto MUTABLE: `uploadFile`/`createDirectory` lo
/// modifican igual que haría el servidor real, para poder encadenar dos
/// `syncNow` en un test y ver el efecto del primero en el segundo.
class _FakeFilesRepository implements FilesRepository {
  final Map<String, DirectoryListing> listingsByPath = {};
  final List<String> downloadedFileIds = [];
  final List<String> uploadedPaths = [];
  final List<String> createdDirs = [];
  final Set<String> failDownloadForIds = {};
  final Set<String> failUploadForNames = {};
  int listCallCount = 0;

  /// Marca de tiempo que el fake asigna a un archivo recién subido -- los
  /// tests la mueven para simular ediciones remotas posteriores.
  DateTime uploadTimestamp = DateTime.utc(2026, 6, 1);

  Completer<void>? listGate;
  int _idCounter = 0;
  final Map<String, List<int>> _contentById = {};

  String _join(String parent, String name) =>
      parent == '/' ? '/$name' : '$parent/$name';

  /// Inserta/actualiza un archivo remoto con contenido real, para que un
  /// `downloadFile` posterior escriba esos bytes (no ceros). Reutiliza el
  /// id si el nombre ya existía, como haría el servidor al versionar en
  /// sitio.
  FileEntry putRemoteFile({
    required String parentPath,
    required String name,
    required List<int> content,
    DateTime? updatedAt,
  }) {
    final ts = updatedAt ?? uploadTimestamp;
    final existing = listingsByPath[parentPath] ??
        const DirectoryListing(directories: [], files: []);
    FileEntry? prior;
    for (final f in existing.files) {
      if (f.name == name) prior = f;
    }
    final id = prior?.id ?? 'seed-${_idCounter++}';
    final entry = FileEntry(
      id: id,
      parentPath: parentPath,
      name: name,
      sizeBytes: content.length,
      sha256: sha256.convert(content).toString(),
      mimeType: 'text/plain',
      createdAt: ts,
      updatedAt: ts,
    );
    _contentById[id] = List.of(content);
    listingsByPath[parentPath] = DirectoryListing(
      directories: existing.directories,
      files: [
        ...existing.files.where((f) => f.name != name),
        entry,
      ],
    );
    return entry;
  }

  @override
  Future<DirectoryListing> list(String path) async {
    listCallCount++;
    final gate = listGate;
    if (gate != null) await gate.future;
    final listing = listingsByPath[path];
    if (listing == null) {
      throw const ApiException(
        code: 'not_found',
        message: 'Carpeta no encontrada.',
      );
    }
    return listing;
  }

  @override
  Future<FileEntry> uploadFile({
    required String parentPath,
    required String localFilePath,
    required String fileName,
    TransferProgress? onProgress,
  }) async {
    if (failUploadForNames.contains(fileName)) {
      throw const ApiException(
        code: 'forbidden',
        message: 'Subida no permitida.',
      );
    }
    final bytes = await File(localFilePath).readAsBytes();
    uploadedPaths.add(_join(parentPath, fileName));

    final existing = listingsByPath[parentPath] ??
        const DirectoryListing(directories: [], files: []);
    FileEntry? prior;
    for (final f in existing.files) {
      if (f.name == fileName) prior = f;
    }
    final id = prior?.id ?? 'up-${_idCounter++}';
    final entry = FileEntry(
      id: id,
      parentPath: parentPath,
      name: fileName,
      sizeBytes: bytes.length,
      sha256: sha256.convert(bytes).toString(),
      mimeType: 'application/octet-stream',
      createdAt: uploadTimestamp,
      updatedAt: uploadTimestamp,
    );
    _contentById[id] = bytes;
    listingsByPath[parentPath] = DirectoryListing(
      directories: existing.directories,
      files: [
        ...existing.files.where((f) => f.name != fileName),
        entry,
      ],
    );
    return entry;
  }

  @override
  Future<void> createDirectory({
    required String parentPath,
    required String name,
  }) async {
    createdDirs.add(_join(parentPath, name));
    final parent = listingsByPath[parentPath] ??
        const DirectoryListing(directories: [], files: []);
    if (!parent.directories.any((d) => d.name == name)) {
      listingsByPath[parentPath] = DirectoryListing(
        directories: [
          ...parent.directories,
          DirectoryEntry(
            id: 'dir-${_idCounter++}',
            parentPath: parentPath,
            name: name,
            createdAt: uploadTimestamp,
          ),
        ],
        files: parent.files,
      );
    }
    listingsByPath.putIfAbsent(
      _join(parentPath, name),
      () => const DirectoryListing(directories: [], files: []),
    );
  }

  @override
  Future<void> downloadFile({
    required FileEntry file,
    required String saveToPath,
    TransferProgress? onProgress,
  }) async {
    downloadedFileIds.add(file.id);
    if (failDownloadForIds.contains(file.id)) {
      throw const ApiException(
        code: 'forbidden',
        message: 'Acceso no permitido.',
      );
    }
    final content = _contentById[file.id] ?? List<int>.filled(file.sizeBytes, 0);
    await File(saveToPath).writeAsBytes(content);
  }

  @override
  Future<void> deleteFile(String fileId, {bool permanent = false}) =>
      throw UnimplementedError();

  @override
  Future<void> deleteDirectory(String directoryId, {bool permanent = false}) =>
      throw UnimplementedError();

  @override
  Future<void> restoreFile(String fileId) => throw UnimplementedError();

  @override
  Future<void> restoreDirectory(String directoryId) => throw UnimplementedError();

  @override
  Future<DirectoryListing> listTrash() => throw UnimplementedError();

  @override
  Future<List<FileVersion>> listVersions(String fileId) =>
      throw UnimplementedError();

  @override
  Future<void> downloadVersion({
    required String fileId,
    required FileVersion version,
    required String saveToPath,
    TransferProgress? onProgress,
  }) =>
      throw UnimplementedError();

  @override
  Future<FileEntry> restoreVersion({
    required String fileId,
    required int versionNum,
  }) =>
      throw UnimplementedError();
}

class _InMemorySyncStateStore implements SyncStateStore {
  final Map<String, Map<String, SyncStateEntry>> byPair = {};

  String _key(SyncPair pair) => '${pair.remotePath}|${pair.localPath}';

  @override
  Future<Map<String, SyncStateEntry>> read(SyncPair pair) async =>
      Map.of(byPair[_key(pair)] ?? const {});

  @override
  Future<void> write(SyncPair pair, Map<String, SyncStateEntry> entries) async =>
      byPair[_key(pair)] = Map.of(entries);
}

FileEntry _file({
  required String id,
  required String parentPath,
  required String name,
  int sizeBytes = 5,
  String sha256 = 'irrelevante-en-este-test',
  DateTime? updatedAt,
}) {
  final ts = updatedAt ?? DateTime.utc(2026, 1, 1);
  return FileEntry(
    id: id,
    parentPath: parentPath,
    name: name,
    sizeBytes: sizeBytes,
    sha256: sha256,
    mimeType: 'text/plain',
    createdAt: ts,
    updatedAt: ts,
  );
}

String _sha(List<int> bytes) => sha256.convert(bytes).toString();

void main() {
  late Directory tempDir;
  late _FakeFilesRepository fake;
  late _InMemorySyncStateStore stateStore;
  late SyncEngine engine;

  setUp(() {
    tempDir = Directory.systemTemp.createTempSync('nexuscloud_sync_');
    fake = _FakeFilesRepository();
    stateStore = _InMemorySyncStateStore();
    engine = SyncEngine(filesRepository: fake, stateStore: stateStore);
  });

  tearDown(() {
    if (tempDir.existsSync()) tempDir.deleteSync(recursive: true);
  });

  // ------------------------------------------------------------------
  // Modo descarga (comportamiento previo -- regresión)
  // ------------------------------------------------------------------

  test('descarga un árbol remoto completo a una carpeta local vacía', () async {
    final fileA = _file(id: 'f-a', parentPath: '/sync-root', name: 'a.txt', sizeBytes: 5);
    final fileB = _file(id: 'f-b', parentPath: '/sync-root/sub', name: 'b.txt', sizeBytes: 7);
    fake.listingsByPath['/sync-root'] = DirectoryListing(
      directories: [
        DirectoryEntry(
          id: 'd-sub',
          parentPath: '/sync-root',
          name: 'sub',
          createdAt: DateTime.utc(2026),
        ),
      ],
      files: [fileA],
    );
    fake.listingsByPath['/sync-root/sub'] =
        DirectoryListing(directories: const [], files: [fileB]);

    final result = await engine.syncNow(
      SyncPair(remotePath: '/sync-root', localPath: tempDir.path),
    );

    expect(result.downloaded, 2);
    expect(result.skipped, 0);
    expect(result.errors, isEmpty);
    expect(File('${tempDir.path}/a.txt').existsSync(), isTrue);
    expect(File('${tempDir.path}/sub/b.txt').existsSync(), isTrue);
    expect(File('${tempDir.path}/a.txt').lengthSync(), 5);
    expect(File('${tempDir.path}/sub/b.txt').lengthSync(), 7);
  });

  test(
    'omite un archivo ya al día (tamaño+fecha coinciden) sin volver a descargarlo',
    () async {
      final updatedAt = DateTime.utc(2026, 3, 1, 10);
      final fileA = _file(
        id: 'f-a',
        parentPath: '/sync-root',
        name: 'a.txt',
        sizeBytes: 5,
        updatedAt: updatedAt,
      );
      fake.listingsByPath['/sync-root'] =
          DirectoryListing(directories: const [], files: [fileA]);

      final localFile = File('${tempDir.path}/a.txt');
      await localFile.writeAsBytes(List.filled(5, 0));
      await localFile.setLastModified(updatedAt);

      final result = await engine.syncNow(
        SyncPair(remotePath: '/sync-root', localPath: tempDir.path),
      );

      expect(result.downloaded, 0);
      expect(result.skipped, 1);
      expect(fake.downloadedFileIds, isEmpty);
    },
  );

  test('descarga de nuevo si el tamaño local ya no coincide', () async {
    final updatedAt = DateTime.utc(2026, 3, 1, 10);
    final fileA = _file(
      id: 'f-a',
      parentPath: '/sync-root',
      name: 'a.txt',
      sizeBytes: 5,
      updatedAt: updatedAt,
    );
    fake.listingsByPath['/sync-root'] =
        DirectoryListing(directories: const [], files: [fileA]);

    final localFile = File('${tempDir.path}/a.txt');
    await localFile.writeAsBytes(List.filled(3, 0)); // tamaño distinto
    await localFile.setLastModified(updatedAt);

    final result = await engine.syncNow(
      SyncPair(remotePath: '/sync-root', localPath: tempDir.path),
    );

    expect(result.downloaded, 1);
    expect(result.skipped, 0);
    expect(fake.downloadedFileIds, ['f-a']);
  });

  test(
    'detecta una colisión de mayúsculas/minúsculas y no descarga ninguno de los dos',
    () async {
      fake.listingsByPath['/sync-root'] = DirectoryListing(
        directories: const [],
        files: [
          _file(id: 'f-upper', parentPath: '/sync-root', name: 'Foto.jpg'),
          _file(id: 'f-lower', parentPath: '/sync-root', name: 'foto.jpg'),
        ],
      );

      final result = await engine.syncNow(
        SyncPair(remotePath: '/sync-root', localPath: tempDir.path),
      );

      expect(result.downloaded, 0);
      expect(result.errors, hasLength(1));
      expect(result.errors.single, contains('mayúsculas'));
      expect(fake.downloadedFileIds, isEmpty);
    },
  );

  test('el fallo de un archivo no aborta la sincronización de los demás', () async {
    fake.failDownloadForIds.add('f-bad');
    fake.listingsByPath['/sync-root'] = DirectoryListing(
      directories: const [],
      files: [
        _file(id: 'f-bad', parentPath: '/sync-root', name: 'malo.txt'),
        _file(id: 'f-good', parentPath: '/sync-root', name: 'bueno.txt'),
      ],
    );

    final result = await engine.syncNow(
      SyncPair(remotePath: '/sync-root', localPath: tempDir.path),
    );

    expect(result.downloaded, 1);
    expect(result.errors, hasLength(1));
    expect(result.errors.single, contains('malo.txt'));
    expect(File('${tempDir.path}/bueno.txt').existsSync(), isTrue);
  });

  test('una raíz remota inexistente propaga la ApiException (fallo de arranque)', () async {
    await expectLater(
      engine.syncNow(SyncPair(remotePath: '/no-existe', localPath: tempDir.path)),
      throwsA(isA<ApiException>().having((e) => e.code, 'code', 'not_found')),
    );
  });

  test(
    'dos llamadas solapadas a syncNow comparten la misma ejecución y no duplican trabajo',
    () async {
      fake.listingsByPath['/sync-root'] = DirectoryListing(
        directories: const [],
        files: [_file(id: 'f-a', parentPath: '/sync-root', name: 'a.txt')],
      );
      fake.listGate = Completer<void>();

      final pair = SyncPair(remotePath: '/sync-root', localPath: tempDir.path);
      final first = engine.syncNow(pair);
      final second = engine.syncNow(pair);

      expect(identical(first, second), isTrue);
      expect(engine.isRunning, isTrue);

      fake.listGate!.complete();
      final results = await Future.wait([first, second]);

      expect(results[0], equals(results[1]));
      expect(results[0].downloaded, 1);
      expect(fake.listCallCount, 1);
      expect(engine.isRunning, isFalse);
    },
  );

  test(
    'onBusyChanged emite true al arrancar y false al terminar, incluso en error',
    () async {
      final events = <bool>[];
      final subscription = engine.onBusyChanged.listen(events.add);

      await expectLater(
        engine.syncNow(SyncPair(remotePath: '/no-existe', localPath: tempDir.path)),
        throwsA(isA<ApiException>()),
      );
      await Future<void>.delayed(Duration.zero);

      expect(events, [true, false]);
      await subscription.cancel();
    },
  );

  // ------------------------------------------------------------------
  // Modo subida
  // ------------------------------------------------------------------

  group('modo upload', () {
    test('sube un archivo local que no está en el servidor', () async {
      fake.listingsByPath['/root'] =
          const DirectoryListing(directories: [], files: []);
      await File('${tempDir.path}/nuevo.txt').writeAsString('hola');

      final result = await engine.syncNow(
        SyncPair(remotePath: '/root', localPath: tempDir.path),
        direction: SyncDirection.upload,
      );

      expect(result.uploaded, 1);
      expect(result.downloaded, 0);
      expect(fake.uploadedPaths, ['/root/nuevo.txt']);
    });

    test('no sube si el contenido ya coincide con el del servidor', () async {
      final bytes = utf8.encode('mismo contenido');
      await File('${tempDir.path}/a.txt').writeAsBytes(bytes);
      fake.listingsByPath['/root'] = DirectoryListing(
        directories: const [],
        files: [
          _file(
            id: 'r-a',
            parentPath: '/root',
            name: 'a.txt',
            sizeBytes: bytes.length,
            sha256: _sha(bytes),
          ),
        ],
      );

      final result = await engine.syncNow(
        SyncPair(remotePath: '/root', localPath: tempDir.path),
        direction: SyncDirection.upload,
      );

      expect(result.uploaded, 0);
      expect(result.skipped, 1);
      expect(fake.uploadedPaths, isEmpty);
    });

    test('sube si el archivo local cambió respecto al del servidor', () async {
      await File('${tempDir.path}/a.txt').writeAsString('version local nueva');
      fake.listingsByPath['/root'] = DirectoryListing(
        directories: const [],
        files: [
          _file(
            id: 'r-a',
            parentPath: '/root',
            name: 'a.txt',
            sizeBytes: 4,
            sha256: 'sha-vieja-distinta',
          ),
        ],
      );

      final result = await engine.syncNow(
        SyncPair(remotePath: '/root', localPath: tempDir.path),
        direction: SyncDirection.upload,
      );

      expect(result.uploaded, 1);
      expect(fake.uploadedPaths, ['/root/a.txt']);
    });

    test('crea las carpetas padre que falten antes de subir un archivo anidado', () async {
      fake.listingsByPath['/root'] =
          const DirectoryListing(directories: [], files: []);
      await Directory('${tempDir.path}/sub/deep').create(recursive: true);
      await File('${tempDir.path}/sub/deep/b.txt').writeAsString('anidado');

      final result = await engine.syncNow(
        SyncPair(remotePath: '/root', localPath: tempDir.path),
        direction: SyncDirection.upload,
      );

      expect(result.uploaded, 1);
      expect(fake.createdDirs, containsAll(['/root/sub', '/root/sub/deep']));
      expect(fake.uploadedPaths, ['/root/sub/deep/b.txt']);
    });

    test('no recorre basura del SO ni las propias conflict copies', () async {
      fake.listingsByPath['/root'] =
          const DirectoryListing(directories: [], files: []);
      await File('${tempDir.path}/real.txt').writeAsString('a');
      await File('${tempDir.path}/Thumbs.db').writeAsString('x');
      await File('${tempDir.path}/x (conflicto 2026-01-02 03.04.05).txt')
          .writeAsString('y');

      final result = await engine.syncNow(
        SyncPair(remotePath: '/root', localPath: tempDir.path),
        direction: SyncDirection.upload,
      );

      expect(result.uploaded, 1);
      expect(fake.uploadedPaths, ['/root/real.txt']);
    });
  });

  // ------------------------------------------------------------------
  // Modo both (reconciliación de tres vías)
  // ------------------------------------------------------------------

  group('modo both', () {
    late SyncPair pair;

    setUp(() {
      pair = SyncPair(remotePath: '/root', localPath: tempDir.path);
      fake.listingsByPath['/root'] =
          const DirectoryListing(directories: [], files: []);
    });

    /// Deja `a.txt` en sync en los dos lados con [content], y siembra la
    /// base del manifiesto en consecuencia.
    Future<void> seedInSync(String content) async {
      final bytes = utf8.encode(content);
      final localFile = File('${tempDir.path}/a.txt');
      await localFile.writeAsBytes(bytes);
      final remoteTs = DateTime.utc(2026, 5, 1, 12);
      await localFile.setLastModified(remoteTs);
      fake.listingsByPath['/root'] =
          const DirectoryListing(directories: [], files: []);
      final remote = fake.putRemoteFile(
        parentPath: '/root',
        name: 'a.txt',
        content: bytes,
        updatedAt: remoteTs,
      );
      await stateStore.write(pair, {
        'a.txt': SyncStateEntry(
          remoteSizeBytes: bytes.length,
          localSizeBytes: bytes.length,
          sha256: remote.sha256,
          remoteUpdatedAt: remoteTs,
          localModifiedAt: localFile.statSync().modified.toUtc(),
        ),
      });
    }

    void bumpRemote(String content) {
      fake.putRemoteFile(
        parentPath: '/root',
        name: 'a.txt',
        content: utf8.encode(content),
        updatedAt: DateTime.utc(2026, 9, 1, 9),
      );
    }

    test('con base: nada cambió -> omite', () async {
      await seedInSync('contenido estable');
      final result = await engine.syncNow(pair, direction: SyncDirection.both);
      expect(result.downloaded, 0);
      expect(result.uploaded, 0);
      expect(result.conflicts, isEmpty);
      expect(result.skipped, 1);
    });

    test('con base: solo el remoto cambió -> descarga', () async {
      await seedInSync('v1');
      bumpRemote('v2 remoto');

      final result = await engine.syncNow(pair, direction: SyncDirection.both);

      expect(result.downloaded, 1);
      expect(result.uploaded, 0);
      expect(
        await File('${tempDir.path}/a.txt').readAsString(),
        'v2 remoto',
      );
    });

    test('con base: solo el local cambió -> sube', () async {
      await seedInSync('v1');
      await File('${tempDir.path}/a.txt').writeAsString('v2 local mas larga');

      final result = await engine.syncNow(pair, direction: SyncDirection.both);

      expect(result.uploaded, 1);
      expect(result.downloaded, 0);
      expect(fake.uploadedPaths, ['/root/a.txt']);
    });

    test(
      'con base: cambió en los dos lados con contenido distinto -> conflict copy, sin tocar local ni servidor',
      () async {
        await seedInSync('v1');
        await File('${tempDir.path}/a.txt').writeAsString('local divergente');
        bumpRemote('remoto divergente');

        final result = await engine.syncNow(pair, direction: SyncDirection.both);

        expect(result.conflicts, ['a.txt']);
        expect(result.uploaded, 0);
        expect(fake.uploadedPaths, isEmpty);
        // El archivo local queda intacto.
        expect(
          await File('${tempDir.path}/a.txt').readAsString(),
          'local divergente',
        );
        // Aparece la conflict copy con el contenido remoto.
        final copies = tempDir
            .listSync()
            .whereType<File>()
            .where((f) => f.path.contains('(conflicto '))
            .toList();
        expect(copies, hasLength(1));
        expect(await copies.single.readAsString(), 'remoto divergente');
      },
    );

    test(
      'con base: cambió en los dos lados pero al mismo contenido -> ni transfiere ni marca conflicto',
      () async {
        await seedInSync('v1');
        // Los dos lados acaban con el MISMO contenido nuevo.
        await File('${tempDir.path}/a.txt').writeAsString('convergencia');
        bumpRemote('convergencia');

        final result = await engine.syncNow(pair, direction: SyncDirection.both);

        expect(result.downloaded, 0);
        expect(result.uploaded, 0);
        expect(result.conflicts, isEmpty);
        expect(result.skipped, 1);
      },
    );

    test('sin base: los dos lados ya tienen el mismo contenido -> solo registra', () async {
      final bytes = utf8.encode('igual desde el principio');
      await File('${tempDir.path}/a.txt').writeAsBytes(bytes);
      fake.listingsByPath['/root'] = DirectoryListing(
        directories: const [],
        files: [
          _file(
            id: 'r-a',
            parentPath: '/root',
            name: 'a.txt',
            sizeBytes: bytes.length,
            sha256: _sha(bytes),
          ),
        ],
      );

      final result = await engine.syncNow(pair, direction: SyncDirection.both);

      expect(result.downloaded, 0);
      expect(result.uploaded, 0);
      expect(result.conflicts, isEmpty);
      expect(result.skipped, 1);
    });

    test('sin base: los dos lados difieren -> conflicto', () async {
      await File('${tempDir.path}/a.txt').writeAsString('local sin base');
      fake.listingsByPath['/root'] = DirectoryListing(
        directories: const [],
        files: [
          _file(
            id: 'r-a',
            parentPath: '/root',
            name: 'a.txt',
            sizeBytes: 4,
            sha256: 'otra-sha',
          ),
        ],
      );

      final result = await engine.syncNow(pair, direction: SyncDirection.both);

      expect(result.conflicts, ['a.txt']);
    });

    test('borrado no propagado: estaba en base y en remoto, falta en local -> se vuelve a bajar', () async {
      await seedInSync('vivo');
      File('${tempDir.path}/a.txt').deleteSync();

      final result = await engine.syncNow(pair, direction: SyncDirection.both);

      expect(result.downloaded, 1);
      expect(File('${tempDir.path}/a.txt').existsSync(), isTrue);
    });

    test('borrado no propagado: estaba en base y en local, falta en remoto -> se vuelve a subir', () async {
      await seedInSync('vivo');
      fake.listingsByPath['/root'] =
          const DirectoryListing(directories: [], files: []);

      final result = await engine.syncNow(pair, direction: SyncDirection.both);

      expect(result.uploaded, 1);
      expect(fake.uploadedPaths, ['/root/a.txt']);
    });

    test('entrada solo en la base (borrada en los dos lados) se suelta sin fallar', () async {
      await stateStore.write(pair, {
        'fantasma.txt': SyncStateEntry(
          remoteSizeBytes: 3,
          localSizeBytes: 3,
          sha256: 'sha',
          remoteUpdatedAt: DateTime.utc(2026, 5, 1),
          localModifiedAt: DateTime.utc(2026, 5, 1),
        ),
      });
      // Ni en remoto (listado vacío) ni en local.

      final result = await engine.syncNow(pair, direction: SyncDirection.both);

      expect(result.downloaded, 0);
      expect(result.uploaded, 0);
      expect(result.errors, isEmpty);
      expect((await stateStore.read(pair)).containsKey('fantasma.txt'), isFalse);
    });

    test('un conflicto ya resuelto en la base no se vuelve a disparar', () async {
      await seedInSync('v1');
      await File('${tempDir.path}/a.txt').writeAsString('local divergente');
      bumpRemote('remoto divergente');

      final first = await engine.syncNow(pair, direction: SyncDirection.both);
      expect(first.conflicts, ['a.txt']);

      // Segunda pasada sin más cambios: la base se avanzó, no re-conflicto.
      final second = await engine.syncNow(pair, direction: SyncDirection.both);
      expect(second.conflicts, isEmpty);
      expect(second.skipped, 1);
    });
  });
}
