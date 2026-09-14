import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/network/api_exception.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_listing.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_entry.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_entry.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_version.dart';
import 'package:nexuscloud_client/features/files/domain/repositories/files_repository.dart';
import 'package:nexuscloud_client/features/sync/data/repositories/file_local_trash_store.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_direction.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_pair.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_pair_config.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_state_entry.dart';
import 'package:nexuscloud_client/features/sync/domain/repositories/sync_state_store.dart';
import 'package:nexuscloud_client/features/sync/domain/services/multi_pair_sync_coordinator.dart';
import 'package:nexuscloud_client/features/sync/domain/services/sync_engine.dart';

/// Solo implementa `list`/`downloadFile`/`deleteFile` de verdad -- esta
/// suite prueba la orquestación de varios pares
/// (`MultiPairSyncCoordinator`), no la lógica interna de una pasada (ya
/// cubierta en `sync_engine_test.dart`); `deleteFile` hace falta para el
/// escenario de borrados confirmados por par.
class _FakeFilesRepository implements FilesRepository {
  final Map<String, DirectoryListing> listingsByPath = {};
  final List<(String, bool)> deleteFileCalls = [];

  @override
  Future<DirectoryListing> list(String path) async {
    final listing = listingsByPath[path];
    if (listing == null) {
      throw const ApiException(code: 'not_found', message: 'Carpeta no encontrada.');
    }
    return listing;
  }

  @override
  Future<void> downloadFile({
    required FileEntry file,
    required String saveToPath,
    TransferProgress? onProgress,
  }) async {
    await File(saveToPath).writeAsBytes(List.filled(file.sizeBytes, 0));
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
  Future<void> createDirectory({required String parentPath, required String name}) =>
      throw UnimplementedError();

  @override
  Future<void> deleteFile(String fileId, {bool permanent = false}) async {
    deleteFileCalls.add((fileId, permanent));
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

  @override
  Future<void> deleteDirectory(String directoryId, {bool permanent = false}) =>
      throw UnimplementedError();

  @override
  Future<FileEntry> moveFile(String fileId, {String? newParentPath, String? newName}) =>
      throw UnimplementedError();

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
  Future<List<FileVersion>> listVersions(String fileId) => throw UnimplementedError();

  @override
  Future<void> downloadVersion({
    required String fileId,
    required FileVersion version,
    required String saveToPath,
    TransferProgress? onProgress,
  }) =>
      throw UnimplementedError();

  @override
  Future<FileEntry> restoreVersion({required String fileId, required int versionNum}) =>
      throw UnimplementedError();
}

class _InMemorySyncStateStore implements SyncStateStore {
  final Map<String, Map<String, SyncStateEntry>> _byPair = {};
  String _key(SyncPair pair) => '${pair.remotePath}|${pair.localPath}';

  @override
  Future<Map<String, SyncStateEntry>> read(SyncPair pair) async =>
      Map.of(_byPair[_key(pair)] ?? const {});

  @override
  Future<void> write(SyncPair pair, Map<String, SyncStateEntry> entries) async =>
      _byPair[_key(pair)] = Map.of(entries);
}

FileEntry _file({required String id, required String parentPath, required String name}) {
  final ts = DateTime.utc(2026, 1, 1);
  return FileEntry(
    id: id,
    parentPath: parentPath,
    name: name,
    sizeBytes: 4,
    sha256: 'irrelevante-en-este-test',
    mimeType: 'text/plain',
    createdAt: ts,
    updatedAt: ts,
  );
}

void main() {
  late Directory tempDir;
  late Directory trashDir;
  late _FakeFilesRepository fake;
  late MultiPairSyncCoordinator coordinator;

  setUp(() {
    tempDir = Directory.systemTemp.createTempSync('nexuscloud_coord_');
    trashDir = Directory.systemTemp.createTempSync('nexuscloud_coord_trash_');
    fake = _FakeFilesRepository();
    final engine = SyncEngine(
      filesRepository: fake,
      stateStore: _InMemorySyncStateStore(),
      trashStore: FileLocalTrashStore(baseDirectoryOverride: trashDir),
    );
    coordinator = MultiPairSyncCoordinator(syncEngine: engine);
  });

  tearDown(() {
    if (tempDir.existsSync()) tempDir.deleteSync(recursive: true);
    if (trashDir.existsSync()) trashDir.deleteSync(recursive: true);
  });

  test('sincroniza varios pares en orden, cada uno con su propio resultado', () async {
    fake.listingsByPath['/root-a'] = DirectoryListing(
      directories: const [],
      files: [_file(id: 'f-a', parentPath: '/root-a', name: 'a.txt')],
    );
    fake.listingsByPath['/root-b'] = DirectoryListing(
      directories: const [],
      files: [_file(id: 'f-b', parentPath: '/root-b', name: 'b.txt')],
    );
    final configs = [
      SyncPairConfig(
        pair: SyncPair(remotePath: '/root-a', localPath: '${tempDir.path}/a'),
        direction: SyncDirection.download,
      ),
      SyncPairConfig(
        pair: SyncPair(remotePath: '/root-b', localPath: '${tempDir.path}/b'),
        direction: SyncDirection.download,
      ),
    ];

    final outcomes = await coordinator.syncAllNow(configs);

    expect(outcomes, hasLength(2));
    expect(outcomes[0].pair.remotePath, '/root-a');
    expect(outcomes[0].result?.downloaded, 1);
    expect(outcomes[0].startupError, isNull);
    expect(outcomes[1].pair.remotePath, '/root-b');
    expect(outcomes[1].result?.downloaded, 1);
  });

  test('un fallo de arranque en un par no impide procesar los demás', () async {
    fake.listingsByPath['/root-b'] = DirectoryListing(
      directories: const [],
      files: [_file(id: 'f-b', parentPath: '/root-b', name: 'b.txt')],
    );
    // /root-a deliberadamente SIN listado -> el fake lanza ApiException.
    final configs = [
      SyncPairConfig(
        pair: SyncPair(remotePath: '/root-a', localPath: '${tempDir.path}/a'),
        direction: SyncDirection.download,
      ),
      SyncPairConfig(
        pair: SyncPair(remotePath: '/root-b', localPath: '${tempDir.path}/b'),
        direction: SyncDirection.download,
      ),
    ];

    final outcomes = await coordinator.syncAllNow(configs);

    expect(outcomes, hasLength(2));
    expect(outcomes[0].startupError, isNotNull);
    expect(outcomes[0].result, isNull);
    // El segundo par SÍ se procesó pese al fallo del primero.
    expect(outcomes[1].startupError, isNull);
    expect(outcomes[1].result?.downloaded, 1);
  });

  test('onStatus recibe el par correcto para cada mensaje', () async {
    fake.listingsByPath['/root-a'] = const DirectoryListing(directories: [], files: []);
    fake.listingsByPath['/root-b'] = const DirectoryListing(directories: [], files: []);
    final pairA = SyncPair(remotePath: '/root-a', localPath: '${tempDir.path}/a');
    final pairB = SyncPair(remotePath: '/root-b', localPath: '${tempDir.path}/b');
    final seenPairs = <SyncPair>[];

    await coordinator.syncAllNow(
      [
        SyncPairConfig(pair: pairA, direction: SyncDirection.download),
        SyncPairConfig(pair: pairB, direction: SyncDirection.download),
      ],
      onStatus: (pair, status) => seenPairs.add(pair),
    );

    expect(seenPairs, isNotEmpty);
    expect(seenPairs.toSet(), {pairA, pairB});
  });

  test('confirmedDeletePathsByPair solo confirma el borrado del par correcto', () async {
    // Dos pares, cada uno con 11 archivos borrados en local (por encima del
    // umbral anti-"borrado masivo" de ADR-013, para que la confirmación
    // importe de verdad) -- solo se confirma el lote del par A.
    final pairA = SyncPair(remotePath: '/root-a', localPath: '${tempDir.path}/a');
    final pairB = SyncPair(remotePath: '/root-b', localPath: '${tempDir.path}/b');
    final ts = DateTime.utc(2026, 5, 1);
    final stateStore = _InMemorySyncStateStore();
    final engine = SyncEngine(
      filesRepository: fake,
      stateStore: stateStore,
      trashStore: FileLocalTrashStore(baseDirectoryOverride: trashDir),
    );
    coordinator = MultiPairSyncCoordinator(syncEngine: engine);
    // La carpeta local debe existir de verdad -- si no, la guarda de
    // "carpeta local inexistente" de ADR-013 aborta la pasada entera.
    await Directory(pairA.localPath).create(recursive: true);
    await Directory(pairB.localPath).create(recursive: true);

    final aKeys = <String>{};
    for (final pair in [pairA, pairB]) {
      final files = <FileEntry>[];
      final entries = <String, SyncStateEntry>{};
      for (var i = 0; i < 11; i++) {
        final name = 'n$i.txt';
        files.add(_file(id: '${pair.remotePath}-$name', parentPath: pair.remotePath, name: name));
        entries[name] = SyncStateEntry(
          remoteSizeBytes: 4,
          localSizeBytes: 4,
          sha256: 'irrelevante-en-este-test',
          remoteUpdatedAt: ts,
          localModifiedAt: ts,
        );
        if (pair == pairA) aKeys.add(name);
        // El archivo local NUNCA se creó -> "borrado local" desde el arranque.
      }
      fake.listingsByPath[pair.remotePath] = DirectoryListing(directories: const [], files: files);
      await stateStore.write(pair, entries);
    }

    final outcomes = await coordinator.syncAllNow(
      [
        SyncPairConfig(pair: pairA, direction: SyncDirection.both),
        SyncPairConfig(pair: pairB, direction: SyncDirection.both),
      ],
      confirmedDeletePathsByPair: {pairA.stableKey: aKeys},
    );

    final outcomeA = outcomes.firstWhere((o) => o.pair == pairA);
    final outcomeB = outcomes.firstWhere((o) => o.pair == pairB);
    // Par A: todo confirmado -> se ejecutan los 11, nada pendiente.
    expect(outcomeA.result?.deletedRemote, 11);
    expect(outcomeA.result?.pendingDeletes, isEmpty);
    // Par B: nada confirmado y por encima del umbral -> nada se ejecuta,
    // los 11 quedan pendientes. Si el enrutado estuviera mal (p.ej. las
    // claves de A se colaran para B) esto fallaría.
    expect(outcomeB.result?.deletedRemote, 0);
    expect(outcomeB.result?.pendingDeletes, hasLength(11));
  });
}
