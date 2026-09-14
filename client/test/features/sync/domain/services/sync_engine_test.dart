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
import 'package:nexuscloud_client/features/sync/data/repositories/file_local_trash_store.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_direction.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_pair.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_state_entry.dart';
import 'package:nexuscloud_client/features/sync/domain/repositories/sync_state_store.dart';
import 'package:nexuscloud_client/features/sync/domain/services/sync_engine.dart';
import 'package:path/path.dart' as p;

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

  /// `(fileId, permanent)` de cada llamada -- los tests de borrados
  /// comprueban que la propagación siempre usa la papelera (`permanent:
  /// false`, ADR-013), nunca un borrado definitivo directo.
  final List<(String, bool)> deleteFileCalls = [];

  @override
  Future<void> deleteFile(String fileId, {bool permanent = false}) async {
    deleteFileCalls.add((fileId, permanent));
    // Igual que haría el servidor real tras mover a la papelera: un
    // recorrido remoto posterior ya no debe volver a ver este archivo.
    for (final path in listingsByPath.keys.toList()) {
      final listing = listingsByPath[path]!;
      if (listing.files.any((f) => f.id == fileId)) {
        listingsByPath[path] = DirectoryListing(
          directories: listing.directories,
          files: listing.files.where((f) => f.id != fileId).toList(),
        );
      }
    }
  }

  final List<String> deletedDirectoryIds = [];

  @override
  Future<void> deleteDirectory(String directoryId, {bool permanent = false}) async {
    deletedDirectoryIds.add(directoryId);
    for (final path in listingsByPath.keys.toList()) {
      final listing = listingsByPath[path]!;
      if (listing.directories.any((d) => d.id == directoryId)) {
        listingsByPath[path] = DirectoryListing(
          directories: listing.directories.where((d) => d.id != directoryId).toList(),
          files: listing.files,
        );
      }
    }
  }

  /// `(fileId, newParentPath, newName)` de cada llamada -- los tests de
  /// detección de rename/move (ADR-030, §85, Parte B) comprueban que
  /// SyncEngine llama aquí en vez de borrar+volver a subir.
  final List<(String, String?, String?)> moveFileCalls = [];
  final Set<String> failMoveForIds = {};

  @override
  Future<FileEntry> moveFile(String fileId, {String? newParentPath, String? newName}) async {
    moveFileCalls.add((fileId, newParentPath, newName));
    if (failMoveForIds.contains(fileId)) {
      throw const ApiException(code: 'forbidden', message: 'Mover no permitido.');
    }
    String? currentParent;
    FileEntry? current;
    for (final entry in listingsByPath.entries) {
      for (final f in entry.value.files) {
        if (f.id == fileId) {
          currentParent = entry.key;
          current = f;
        }
      }
    }
    if (current == null || currentParent == null) {
      throw const ApiException(code: 'not_found', message: 'Archivo no encontrado.');
    }
    final targetParent = newParentPath ?? currentParent;
    final targetName = newName ?? current.name;

    final oldListing = listingsByPath[currentParent]!;
    listingsByPath[currentParent] = DirectoryListing(
      directories: oldListing.directories,
      files: oldListing.files.where((f) => f.id != fileId).toList(),
    );
    // ADR-030: Move nunca cambia updatedAt -- no es un cambio de
    // contenido, así que ni el sha256 ni la fecha de "última
    // modificación de verdad" tienen por qué moverse.
    final updated = FileEntry(
      id: current.id,
      parentPath: targetParent,
      name: targetName,
      sizeBytes: current.sizeBytes,
      sha256: current.sha256,
      mimeType: current.mimeType,
      createdAt: current.createdAt,
      updatedAt: current.updatedAt,
    );
    final targetListing = listingsByPath[targetParent] ?? const DirectoryListing(directories: [], files: []);
    listingsByPath[targetParent] = DirectoryListing(
      directories: targetListing.directories,
      files: [...targetListing.files, updated],
    );
    return updated;
  }

  @override
  Future<DirectoryEntry> moveDirectory(String directoryId, {String? newParentPath, String? newName}) =>
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
  late Directory trashDir;
  late _FakeFilesRepository fake;
  late _InMemorySyncStateStore stateStore;
  late FileLocalTrashStore trashStore;
  late SyncEngine engine;

  setUp(() {
    tempDir = Directory.systemTemp.createTempSync('nexuscloud_sync_');
    trashDir = Directory.systemTemp.createTempSync('nexuscloud_trash_');
    fake = _FakeFilesRepository();
    stateStore = _InMemorySyncStateStore();
    trashStore = FileLocalTrashStore(baseDirectoryOverride: trashDir);
    engine = SyncEngine(filesRepository: fake, stateStore: stateStore, trashStore: trashStore);
  });

  tearDown(() {
    if (tempDir.existsSync()) tempDir.deleteSync(recursive: true);
    if (trashDir.existsSync()) trashDir.deleteSync(recursive: true);
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
    'dos pares DISTINTOS con syncNow solapado se sincronizan los dos de verdad (slice 15, ADR-014)',
    () async {
      // Antes de la reentrancia por par, un único campo `_inFlight` habría
      // fusionado esta segunda llamada con la primera -- el par B nunca se
      // sincronizaría de verdad, en silencio (ver ADR-014).
      fake.listingsByPath['/root-a'] = DirectoryListing(
        directories: const [],
        files: [_file(id: 'f-a', parentPath: '/root-a', name: 'a.txt', sizeBytes: 3)],
      );
      fake.listingsByPath['/root-b'] = DirectoryListing(
        directories: const [],
        files: [_file(id: 'f-b', parentPath: '/root-b', name: 'b.txt', sizeBytes: 5)],
      );
      fake.listGate = Completer<void>();

      final pairA = SyncPair(remotePath: '/root-a', localPath: '${tempDir.path}/a');
      final pairB = SyncPair(remotePath: '/root-b', localPath: '${tempDir.path}/b');

      final futureA = engine.syncNow(pairA);
      final futureB = engine.syncNow(pairB);

      // Futures DISTINTAS -- la guarda antigua las habría fusionado en una.
      expect(identical(futureA, futureB), isFalse);
      expect(engine.isRunning, isTrue);

      fake.listGate!.complete();
      final resultA = await futureA;
      final resultB = await futureB;

      expect(resultA.downloaded, 1);
      expect(resultB.downloaded, 1);
      expect(fake.listCallCount, 2);
      expect(File('${tempDir.path}/a/a.txt').existsSync(), isTrue);
      expect(File('${tempDir.path}/b/b.txt').existsSync(), isTrue);
      expect(engine.isRunning, isFalse);
    },
  );

  group('isPairRunning (slice 17, LocalChangeWatcherService)', () {
    test('false antes de sincronizar', () {
      const pair = SyncPair(remotePath: '/x', localPath: '/y');
      expect(engine.isPairRunning(pair), isFalse);
    });

    test('true mientras ese par está en curso, false al terminar', () async {
      fake.listingsByPath['/root'] = const DirectoryListing(directories: [], files: []);
      fake.listGate = Completer<void>();
      final pair = SyncPair(remotePath: '/root', localPath: '${tempDir.path}/x');

      final future = engine.syncNow(pair);
      expect(engine.isPairRunning(pair), isTrue);

      fake.listGate!.complete();
      await future;
      expect(engine.isPairRunning(pair), isFalse);
    });

    test('un par en curso no afecta a isPairRunning de OTRO par (aislamiento)', () async {
      fake.listingsByPath['/root-a'] = const DirectoryListing(directories: [], files: []);
      fake.listGate = Completer<void>();
      final pairA = SyncPair(remotePath: '/root-a', localPath: '${tempDir.path}/a');
      final pairB = SyncPair(remotePath: '/root-b', localPath: '${tempDir.path}/b');

      final future = engine.syncNow(pairA);
      expect(engine.isPairRunning(pairA), isTrue);
      expect(engine.isPairRunning(pairB), isFalse);

      fake.listGate!.complete();
      await future;
    });
  });

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

    test(
      'borrado local (con base) se propaga: se borra también en el servidor, a la papelera',
      () async {
        await seedInSync('vivo');

        File('${tempDir.path}/a.txt').deleteSync();

        final result = await engine.syncNow(pair, direction: SyncDirection.both);

        expect(result.downloaded, 0);
        expect(result.deletedRemote, 1);
        expect(result.pendingDeletes, isEmpty);
        expect(fake.deleteFileCalls, hasLength(1));
        expect(fake.deleteFileCalls.single.$2, isFalse); // permanent: false
        expect((await stateStore.read(pair)).containsKey('a.txt'), isFalse);
      },
    );

    test(
      'borrado remoto (con base) se propaga: el archivo local se mueve a la papelera local',
      () async {
        await seedInSync('vivo');

        fake.listingsByPath['/root'] =
            const DirectoryListing(directories: [], files: []);

        final result = await engine.syncNow(pair, direction: SyncDirection.both);

        expect(result.uploaded, 0);
        expect(result.deletedLocal, 1);
        expect(result.pendingDeletes, isEmpty);
        expect(File('${tempDir.path}/a.txt').existsSync(), isFalse);
        final trashRoot =
            Directory(p.join(trashDir.path, 'local_trash', pair.stableKey));
        final trashed = trashRoot.listSync().whereType<File>().toList();
        expect(trashed, hasLength(1));
        expect(await trashed.single.readAsString(), 'vivo');
        expect((await stateStore.read(pair)).containsKey('a.txt'), isFalse);
      },
    );

    test(
      'sin base, ausente en un lado -> sigue siendo "nuevo" (nunca candidato a borrado)',
      () async {
        fake.listingsByPath['/root'] = DirectoryListing(
          directories: const [],
          files: [_file(id: 'r-new', parentPath: '/root', name: 'nuevo.txt', sizeBytes: 4)],
        );

        final result = await engine.syncNow(pair, direction: SyncDirection.both);

        expect(result.downloaded, 1);
        expect(result.deletedRemote, 0);
        expect(result.pendingDeletes, isEmpty);
        expect(File('${tempDir.path}/nuevo.txt').existsSync(), isTrue);
      },
    );

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

  group('detección de renombrado/movido (ADR-030, §85, Parte B)', () {
    late SyncPair pair;

    setUp(() {
      pair = SyncPair(remotePath: '/root', localPath: tempDir.path);
      fake.listingsByPath['/root'] =
          const DirectoryListing(directories: [], files: []);
    });

    /// Mismo helper que en 'modo both' (duplicado deliberadamente: es un
    /// closure local a ese grupo, extraerlo a main() tocaría tests ya
    /// verificados sin necesidad real).
    Future<void> seedInSync(String content, {String name = 'a.txt'}) async {
      final bytes = utf8.encode(content);
      final localFile = File('${tempDir.path}/$name');
      await localFile.writeAsBytes(bytes);
      final remoteTs = DateTime.utc(2026, 5, 1, 12);
      await localFile.setLastModified(remoteTs);
      final remote = fake.putRemoteFile(
        parentPath: '/root',
        name: name,
        content: bytes,
        updatedAt: remoteTs,
      );
      await stateStore.write(pair, {
        ...await stateStore.read(pair),
        name: SyncStateEntry(
          remoteSizeBytes: bytes.length,
          localSizeBytes: bytes.length,
          sha256: remote.sha256,
          remoteUpdatedAt: remoteTs,
          localModifiedAt: localFile.statSync().modified.toUtc(),
        ),
      });
    }

    test(
      'rename LOCAL (mismo contenido, desaparece a.txt local, aparece b.txt local) mueve en el servidor, nunca borra+sube',
      () async {
        await seedInSync('contenido idéntico');
        final remoteIdBefore = fake.listingsByPath['/root']!.files.single.id;

        File('${tempDir.path}/a.txt').renameSync('${tempDir.path}/b.txt');

        final result = await engine.syncNow(pair, direction: SyncDirection.both);

        expect(result.moved, 1);
        expect(result.deletedRemote, 0);
        expect(result.uploaded, 0);
        expect(result.pendingDeletes, isEmpty);
        expect(fake.moveFileCalls, hasLength(1));
        expect(fake.moveFileCalls.single.$1, remoteIdBefore);
        expect(fake.moveFileCalls.single.$3, 'b.txt');
        expect(fake.deleteFileCalls, isEmpty);

        final remoteNames = fake.listingsByPath['/root']!.files.map((f) => f.name);
        expect(remoteNames, ['b.txt']);
        expect((await stateStore.read(pair)).containsKey('b.txt'), isTrue);
        expect((await stateStore.read(pair)).containsKey('a.txt'), isFalse);
      },
    );

    test(
      'rename REMOTO (mismo contenido, desaparece a.txt remoto, aparece b.txt remoto) renombra en local, nunca descarga de nuevo',
      () async {
        await seedInSync('contenido idéntico');

        // Simula que OTRO cliente/la web renombró el archivo en el
        // servidor: la clave vieja desaparece del listado remoto, aparece
        // una nueva con el MISMO contenido.
        fake.listingsByPath['/root'] =
            const DirectoryListing(directories: [], files: []);
        fake.putRemoteFile(
          parentPath: '/root',
          name: 'b.txt',
          content: utf8.encode('contenido idéntico'),
          updatedAt: DateTime.utc(2026, 5, 1, 12),
        );

        final result = await engine.syncNow(pair, direction: SyncDirection.both);

        expect(result.moved, 1);
        expect(result.deletedLocal, 0);
        expect(result.downloaded, 0);
        expect(result.pendingDeletes, isEmpty);
        expect(fake.downloadedFileIds, isEmpty);
        expect(File('${tempDir.path}/a.txt').existsSync(), isFalse);
        expect(File('${tempDir.path}/b.txt').existsSync(), isTrue);
        expect(
          await File('${tempDir.path}/b.txt').readAsString(),
          'contenido idéntico',
        );
        expect((await stateStore.read(pair)).containsKey('b.txt'), isTrue);
        expect((await stateStore.read(pair)).containsKey('a.txt'), isFalse);
      },
    );

    test(
      'contenido DISTINTO entre el desaparecido y el nuevo -> nunca se detecta como move, sigue el camino normal',
      () async {
        await seedInSync('contenido original');

        File('${tempDir.path}/a.txt').deleteSync();
        await File('${tempDir.path}/b.txt').writeAsString('contenido totalmente distinto');

        final result = await engine.syncNow(pair, direction: SyncDirection.both);

        expect(result.moved, 0);
        expect(result.deletedRemote, 1); // a.txt sí se borró en remoto
        expect(result.uploaded, 1); // b.txt sí se subió como archivo nuevo
        expect(fake.moveFileCalls, isEmpty);
      },
    );

    test(
      'un lote de renombrados que superaría el umbral anti-"borrado masivo" no pide confirmación -- los moves no cuentan como borrados (ADR-013)',
      () async {
        for (var i = 0; i < 15; i++) {
          await seedInSync('contenido $i', name: 'viejo-$i.txt');
        }

        for (var i = 0; i < 15; i++) {
          File('${tempDir.path}/viejo-$i.txt').renameSync('${tempDir.path}/nuevo-$i.txt');
        }

        final result = await engine.syncNow(pair, direction: SyncDirection.both);

        expect(result.moved, 15);
        expect(result.pendingDeletes, isEmpty);
        expect(result.deletedRemote, 0);
        expect(fake.moveFileCalls, hasLength(15));
      },
    );

    test(
      'si moveFile falla en el servidor, cae al camino normal (borra+sube) en vez de perder el archivo',
      () async {
        await seedInSync('contenido idéntico');
        final remoteId = fake.listingsByPath['/root']!.files.single.id;
        fake.failMoveForIds.add(remoteId);

        File('${tempDir.path}/a.txt').renameSync('${tempDir.path}/b.txt');

        final result = await engine.syncNow(pair, direction: SyncDirection.both);

        expect(result.moved, 0);
        expect(result.errors, isNotEmpty);
        // Cayó al camino normal: se borra el viejo en remoto y se sube el
        // nuevo -- el archivo no se pierde solo porque el move fallara.
        expect(result.deletedRemote, 1);
        expect(result.uploaded, 1);
      },
    );
  });

  group('limpieza de carpetas vacías tras un borrado propagado (#22)', () {
    late SyncPair pair;

    setUp(() {
      pair = SyncPair(remotePath: '/root', localPath: tempDir.path);
      fake.listingsByPath['/root'] =
          const DirectoryListing(directories: [], files: []);
    });

    test('borrado local propagado deja vacía una carpeta REMOTA -> la carpeta remota se borra también', () async {
      final bytes = utf8.encode('contenido anidado');
      fake.listingsByPath['/root'] = DirectoryListing(
        directories: [
          DirectoryEntry(id: 'dir-sub', parentPath: '/root', name: 'Sub', createdAt: DateTime.utc(2026)),
        ],
        files: const [],
      );
      final remote = fake.putRemoteFile(
        parentPath: '/root/Sub',
        name: 'a.txt',
        content: bytes,
        updatedAt: DateTime.utc(2026, 5, 1, 12),
      );
      final localFile = File('${tempDir.path}/Sub/a.txt');
      await localFile.create(recursive: true);
      await localFile.writeAsBytes(bytes);
      await localFile.setLastModified(DateTime.utc(2026, 5, 1, 12));
      await stateStore.write(pair, {
        'sub/a.txt': SyncStateEntry(
          remoteSizeBytes: bytes.length,
          localSizeBytes: bytes.length,
          sha256: remote.sha256,
          remoteUpdatedAt: DateTime.utc(2026, 5, 1, 12),
          localModifiedAt: localFile.statSync().modified.toUtc(),
        ),
      });

      // Desaparece en LOCAL -> se propaga como borrado en remoto -> la
      // carpeta remota /root/Sub se queda vacía -> debe limpiarse sola.
      localFile.deleteSync();

      final result = await engine.syncNow(pair, direction: SyncDirection.both);

      expect(result.deletedRemote, 1);
      expect(fake.deletedDirectoryIds, ['dir-sub']);
      // El directorio ya no aparece en el listado de SU PADRE -- lo único
      // que importa funcionalmente (el fake nunca borra la clave del mapa
      // en sí, solo la entrada dentro del listado del padre, igual que el
      // servidor real: la carpeta pasa a la papelera/desaparece, pero
      // "listar dentro de un id que ya no existe" es un caso aparte que
      // este fake no modela).
      expect(fake.listingsByPath['/root']!.directories, isEmpty);
    });

    test('la RAÍZ del par nunca se borra aunque quede vacía', () async {
      final bytes = utf8.encode('único archivo');
      final remote = fake.putRemoteFile(
        parentPath: '/root',
        name: 'a.txt',
        content: bytes,
        updatedAt: DateTime.utc(2026, 5, 1, 12),
      );
      final localFile = File('${tempDir.path}/a.txt');
      await localFile.writeAsBytes(bytes);
      await localFile.setLastModified(DateTime.utc(2026, 5, 1, 12));
      await stateStore.write(pair, {
        'a.txt': SyncStateEntry(
          remoteSizeBytes: bytes.length,
          localSizeBytes: bytes.length,
          sha256: remote.sha256,
          remoteUpdatedAt: DateTime.utc(2026, 5, 1, 12),
          localModifiedAt: localFile.statSync().modified.toUtc(),
        ),
      });

      localFile.deleteSync();

      final result = await engine.syncNow(pair, direction: SyncDirection.both);

      expect(result.deletedRemote, 1);
      // Nunca se intenta borrar la propia raíz del par -- ni siquiera se
      // lista para comprobarlo (relPath de 'a.txt' sin más segmentos de
      // carpeta padre por encima de la raíz).
      expect(fake.deletedDirectoryIds, isEmpty);
    });
  });

  group('modo both -- guarda anti-"borrado masivo" (ADR-013)', () {
    late SyncPair pair;

    setUp(() {
      pair = SyncPair(remotePath: '/root', localPath: tempDir.path);
      fake.listingsByPath['/root'] =
          const DirectoryListing(directories: [], files: []);
    });

    /// Deja `n0.txt`..`n{count-1}.txt` sincronizados en los dos lados
    /// (remoto real vía el fake + local real en disco + base sembrada).
    Future<void> seedManyInSync(int count) async {
      final ts = DateTime.utc(2026, 5, 1, 12);
      final entries = <String, SyncStateEntry>{};
      final files = <FileEntry>[];
      for (var i = 0; i < count; i++) {
        final name = 'n$i.txt';
        final bytes = utf8.encode('contenido $i');
        final localFile = File(p.join(tempDir.path, name));
        await localFile.writeAsBytes(bytes);
        await localFile.setLastModified(ts);
        files.add(_file(
          id: 'r-$i',
          parentPath: '/root',
          name: name,
          sizeBytes: bytes.length,
          sha256: _sha(bytes),
          updatedAt: ts,
        ));
        entries[name] = SyncStateEntry(
          remoteSizeBytes: bytes.length,
          localSizeBytes: bytes.length,
          sha256: _sha(bytes),
          remoteUpdatedAt: ts,
          localModifiedAt: ts,
        );
      }
      fake.listingsByPath['/root'] =
          DirectoryListing(directories: const [], files: files);
      await stateStore.write(pair, entries);
    }

    test(
      'un lote de más de 10 borrados no se ejecuta sin confirmar, y es idempotente',
      () async {
        await seedManyInSync(12);
        for (var i = 0; i < 12; i++) {
          File(p.join(tempDir.path, 'n$i.txt')).deleteSync();
        }

        final first = await engine.syncNow(pair, direction: SyncDirection.both);
        expect(first.deletedRemote, 0);
        expect(first.deletedLocal, 0);
        expect(first.pendingDeletes, hasLength(12));
        expect(fake.deleteFileCalls, isEmpty);

        // Repetir sin confirmar reproduce el mismo resultado -- el candidato
        // sigue ahí porque la base no se tocó.
        final second = await engine.syncNow(pair, direction: SyncDirection.both);
        expect(second.pendingDeletes, hasLength(12));
      },
    );

    test(
      'confirmar un subconjunto ejecuta solo esos; el resto sigue pendiente',
      () async {
        await seedManyInSync(12);
        for (var i = 0; i < 12; i++) {
          File(p.join(tempDir.path, 'n$i.txt')).deleteSync();
        }
        final first = await engine.syncNow(pair, direction: SyncDirection.both);
        final toConfirm = first.pendingDeletes.take(3).map((d) => d.key).toSet();

        final second = await engine.syncNow(
          pair,
          direction: SyncDirection.both,
          confirmedDeletePaths: toConfirm,
        );

        expect(second.deletedRemote, 3);
        expect(second.pendingDeletes, hasLength(9));
        expect(fake.deleteFileCalls, hasLength(3));
      },
    );

    test(
      'exactamente en el umbral (10) se ejecuta sin pedir confirmación',
      () async {
        await seedManyInSync(10);
        for (var i = 0; i < 10; i++) {
          File(p.join(tempDir.path, 'n$i.txt')).deleteSync();
        }

        final result = await engine.syncNow(pair, direction: SyncDirection.both);

        expect(result.deletedRemote, 10);
        expect(result.pendingDeletes, isEmpty);
      },
    );

    test(
      'updateMaxAutoDeleteBatch (#23) cambia el umbral de una instancia ya '
      'construida -- un lote que antes quedaba pendiente pasa a ejecutarse',
      () async {
        await seedManyInSync(12);
        for (var i = 0; i < 12; i++) {
          File(p.join(tempDir.path, 'n$i.txt')).deleteSync();
        }

        // Con el default (10) del `setUp`, 12 quedarían pendientes -- ver el
        // primer test de este grupo. Subir el umbral a 15 en la MISMA
        // instancia (sin reconstruir `engine`) debe bastar para que el
        // siguiente sync ejecute los 12 sin pedir confirmación.
        engine.updateMaxAutoDeleteBatch(15);

        final result = await engine.syncNow(pair, direction: SyncDirection.both);

        expect(result.deletedRemote, 12);
        expect(result.pendingDeletes, isEmpty);
      },
    );

    test(
      'carpeta local inexistente con manifiesto no vacío: no borra nada y conserva la base tal cual',
      () async {
        await seedManyInSync(3);
        final baselineBefore = await stateStore.read(pair);

        tempDir.deleteSync(recursive: true);

        final result = await engine.syncNow(pair, direction: SyncDirection.both);

        expect(result.deletedRemote, 0);
        expect(result.deletedLocal, 0);
        expect(result.errors, isNotEmpty);
        expect(fake.deleteFileCalls, isEmpty);
        expect(await stateStore.read(pair), baselineBefore);
      },
    );
  });

  group('alcance: Descargar/Subir nunca propagan borrados (ADR-013)', () {
    test(
      'download: un archivo que desaparece del remoto no toca la copia local ya descargada',
      () async {
        final pair = SyncPair(remotePath: '/root', localPath: tempDir.path);
        fake.listingsByPath['/root'] = DirectoryListing(
          directories: const [],
          files: [_file(id: 'r-a', parentPath: '/root', name: 'a.txt', sizeBytes: 5)],
        );
        await engine.syncNow(pair, direction: SyncDirection.download);
        expect(File('${tempDir.path}/a.txt').existsSync(), isTrue);

        // Simula que se borró en el servidor.
        fake.listingsByPath['/root'] =
            const DirectoryListing(directories: [], files: []);

        final result = await engine.syncNow(pair, direction: SyncDirection.download);

        expect(result.deletedLocal, 0);
        expect(File('${tempDir.path}/a.txt').existsSync(), isTrue);
      },
    );

    test(
      'upload: un archivo que se borra en local no toca la copia ya subida al servidor',
      () async {
        final pair = SyncPair(remotePath: '/root', localPath: tempDir.path);
        fake.listingsByPath['/root'] =
            const DirectoryListing(directories: [], files: []);
        await File('${tempDir.path}/a.txt').writeAsString('hola');
        await engine.syncNow(pair, direction: SyncDirection.upload);
        expect(fake.uploadedPaths, ['/root/a.txt']);

        File('${tempDir.path}/a.txt').deleteSync();

        final result = await engine.syncNow(pair, direction: SyncDirection.upload);

        expect(result.deletedRemote, 0);
        expect(fake.deleteFileCalls, isEmpty);
        expect(fake.listingsByPath['/root']!.files, hasLength(1));
      },
    );
  });
}
