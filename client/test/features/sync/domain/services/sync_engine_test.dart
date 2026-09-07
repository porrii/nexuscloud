import 'dart:async';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/network/api_exception.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_entry.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_listing.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_entry.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_version.dart';
import 'package:nexuscloud_client/features/files/domain/repositories/files_repository.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_pair.dart';
import 'package:nexuscloud_client/features/sync/domain/services/sync_engine.dart';

class _FakeFilesRepository implements FilesRepository {
  final Map<String, DirectoryListing> listingsByPath = {};
  final List<String> downloadedFileIds = [];
  final Set<String> failForFileIds = {};
  int listCallCount = 0;

  /// Si no es null, cada llamada a [list] espera a que se complete antes
  /// de devolver nada -- usado para mantener una sincronización "en
  /// curso" a propósito mientras un test dispara una segunda llamada a
  /// `syncNow` y comprueba la guarda de reentrancia.
  Completer<void>? listGate;

  @override
  Future<DirectoryListing> list(String path) async {
    listCallCount++;
    final gate = listGate;
    if (gate != null) await gate.future;
    final listing = listingsByPath[path];
    if (listing == null) {
      throw const ApiException(code: 'not_found', message: 'Carpeta no encontrada.');
    }
    return listing;
  }

  @override
  Future<FileEntry> uploadFile({
    required String parentPath,
    required String localFilePath,
    required String fileName,
    TransferProgress? onProgress,
  }) =>
      throw UnimplementedError();

  @override
  Future<void> downloadFile({
    required FileEntry file,
    required String saveToPath,
    TransferProgress? onProgress,
  }) async {
    downloadedFileIds.add(file.id);
    if (failForFileIds.contains(file.id)) {
      throw const ApiException(code: 'forbidden', message: 'Acceso no permitido.');
    }
    // Tamaño real == sizeBytes, para que el chequeo de "ya al día" de una
    // sincronización posterior sea comprobable en los tests -- el
    // contenido en sí es irrelevante aquí (Slice 2 ya prueba la descarga
    // real).
    await File(saveToPath).writeAsBytes(List.filled(file.sizeBytes, 0));
  }

  // El motor de sync (slice A) es de solo lectura hacia el servidor --
  // nunca borra/restaura nada. No lo ejercita ningún test de este archivo.
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

FileEntry _file({
  required String id,
  required String parentPath,
  required String name,
  int sizeBytes = 5,
  DateTime? updatedAt,
}) {
  final ts = updatedAt ?? DateTime.utc(2026, 1, 1);
  return FileEntry(
    id: id,
    parentPath: parentPath,
    name: name,
    sizeBytes: sizeBytes,
    sha256: 'irrelevante-en-este-test',
    mimeType: 'text/plain',
    createdAt: ts,
    updatedAt: ts,
  );
}

void main() {
  late Directory tempDir;
  late _FakeFilesRepository fake;
  late SyncEngine engine;

  setUp(() {
    tempDir = Directory.systemTemp.createTempSync('nexuscloud_sync_');
    fake = _FakeFilesRepository();
    engine = SyncEngine(filesRepository: fake);
  });

  tearDown(() {
    if (tempDir.existsSync()) tempDir.deleteSync(recursive: true);
  });

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
    fake.failForFileIds.add('f-bad');
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

      // Misma Future exacta -- la segunda llamada no arrancó un recorrido
      // nuevo, se unió a la ya en curso.
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
      // El `false` lo dispara un `whenComplete` sobre una Future aparte de
      // la que se acaba de esperar arriba -- un giro de vuelta al event
      // loop de margen para que también termine de entregarse al stream.
      await Future<void>.delayed(Duration.zero);

      expect(events, [true, false]);
      await subscription.cancel();
    },
  );
}
