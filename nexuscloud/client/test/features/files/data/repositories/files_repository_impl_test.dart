import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/network/api_exception.dart';
import 'package:nexuscloud_client/features/files/data/datasources/files_remote_data_source.dart';
import 'package:nexuscloud_client/features/files/data/repositories/files_repository_impl.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_entry.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_listing.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_entry.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_version.dart';
import 'package:nexuscloud_client/features/files/domain/repositories/files_repository.dart';

class _FakeFilesRemoteDataSource implements FilesRemoteDataSource {
  final Map<String, DirectoryListing> listingsByPath = {};
  ApiException? errorToThrow;

  FileEntry? uploadResult;
  ApiException? uploadError;
  final List<String> uploadedFileNames = [];
  final List<int> uploadProgressCalls = [];

  ApiException? downloadError;
  final List<String> downloadedFileIds = [];

  @override
  Future<DirectoryListing> list(String path) async {
    if (errorToThrow != null) throw errorToThrow!;
    return listingsByPath[path] ??
        const DirectoryListing(directories: [], files: []);
  }

  @override
  Future<FileEntry> uploadFile({
    required String parentPath,
    required String localFilePath,
    required String fileName,
    TransferProgress? onProgress,
  }) async {
    uploadedFileNames.add(fileName);
    onProgress?.call(1, 1);
    uploadProgressCalls.add(1);
    if (uploadError != null) throw uploadError!;
    return uploadResult!;
  }

  final List<String> createdDirectories = [];

  @override
  Future<void> createDirectory({
    required String parentPath,
    required String name,
  }) async {
    createdDirectories.add('$parentPath::$name');
  }

  @override
  Future<void> downloadFile({
    required FileEntry file,
    required String saveToPath,
    TransferProgress? onProgress,
  }) async {
    downloadedFileIds.add(file.id);
    onProgress?.call(1, 1);
    if (downloadError != null) throw downloadError!;
  }

  ApiException? deleteError;
  ApiException? restoreError;
  DirectoryListing? trashListing;
  ApiException? trashError;
  final List<String> deletedFileIds = [];
  final List<bool> deletedFilePermanentFlags = [];
  final List<String> deletedDirectoryIds = [];
  final List<bool> deletedDirectoryPermanentFlags = [];
  final List<String> restoredFileIds = [];
  final List<String> restoredDirectoryIds = [];

  @override
  Future<void> deleteFile(String fileId, {bool permanent = false}) async {
    if (deleteError != null) throw deleteError!;
    deletedFileIds.add(fileId);
    deletedFilePermanentFlags.add(permanent);
  }

  @override
  Future<void> deleteDirectory(String directoryId, {bool permanent = false}) async {
    if (deleteError != null) throw deleteError!;
    deletedDirectoryIds.add(directoryId);
    deletedDirectoryPermanentFlags.add(permanent);
  }

  FileEntry? moveFileResult;
  ApiException? moveFileError;
  final List<String> movedFileIds = [];
  final List<String?> movedFileNewParentPaths = [];
  final List<String?> movedFileNewNames = [];
  DirectoryEntry? moveDirectoryResult;
  ApiException? moveDirectoryError;
  final List<String> movedDirectoryIds = [];

  @override
  Future<FileEntry> moveFile(String fileId, {String? newParentPath, String? newName}) async {
    movedFileIds.add(fileId);
    movedFileNewParentPaths.add(newParentPath);
    movedFileNewNames.add(newName);
    if (moveFileError != null) throw moveFileError!;
    return moveFileResult!;
  }

  @override
  Future<DirectoryEntry> moveDirectory(String directoryId, {String? newParentPath, String? newName}) async {
    movedDirectoryIds.add(directoryId);
    if (moveDirectoryError != null) throw moveDirectoryError!;
    return moveDirectoryResult!;
  }

  @override
  Future<void> restoreFile(String fileId) async {
    if (restoreError != null) throw restoreError!;
    restoredFileIds.add(fileId);
  }

  @override
  Future<void> restoreDirectory(String directoryId) async {
    if (restoreError != null) throw restoreError!;
    restoredDirectoryIds.add(directoryId);
  }

  @override
  Future<DirectoryListing> listTrash() async {
    if (trashError != null) throw trashError!;
    return trashListing ?? const DirectoryListing(directories: [], files: []);
  }

  List<FileVersion>? versions;
  ApiException? versionsError;
  ApiException? downloadVersionError;
  final List<String> downloadedVersionFileIds = [];
  final List<int> downloadedVersionNums = [];
  FileEntry? restoreVersionResult;
  ApiException? restoreVersionError;
  final List<String> restoreVersionFileIds = [];
  final List<int> restoreVersionNums = [];

  @override
  Future<List<FileVersion>> listVersions(String fileId) async {
    if (versionsError != null) throw versionsError!;
    return versions ?? const [];
  }

  @override
  Future<void> downloadVersion({
    required String fileId,
    required FileVersion version,
    required String saveToPath,
    TransferProgress? onProgress,
  }) async {
    downloadedVersionFileIds.add(fileId);
    downloadedVersionNums.add(version.versionNum);
    onProgress?.call(1, 1);
    if (downloadVersionError != null) throw downloadVersionError!;
  }

  @override
  Future<FileEntry> restoreVersion({
    required String fileId,
    required int versionNum,
  }) async {
    restoreVersionFileIds.add(fileId);
    restoreVersionNums.add(versionNum);
    if (restoreVersionError != null) throw restoreVersionError!;
    return restoreVersionResult!;
  }
}

void main() {
  final testFile = FileEntry(
    id: 'f1',
    parentPath: '/',
    name: 'foto.jpg',
    sizeBytes: 1024,
    sha256: 'abc123',
    mimeType: 'image/jpeg',
    createdAt: DateTime.utc(2026),
    updatedAt: DateTime.utc(2026),
  );

  test('list delega en el data source y devuelve tal cual el resultado', () async {
    final fake = _FakeFilesRemoteDataSource();
    const listing = DirectoryListing(directories: [], files: []);
    fake.listingsByPath['/Documentos'] = listing;
    final repo = FilesRepositoryImpl(remoteDataSource: fake);

    final result = await repo.list('/Documentos');

    expect(result, listing);
  });

  test('una ApiException del data source se propaga sin cambios', () async {
    final fake = _FakeFilesRemoteDataSource()
      ..errorToThrow = const ApiException(code: 'forbidden', message: 'x');
    final repo = FilesRepositoryImpl(remoteDataSource: fake);

    await expectLater(
      repo.list('/'),
      throwsA(isA<ApiException>().having((e) => e.code, 'code', 'forbidden')),
    );
  });

  test('uploadFile delega y devuelve el FileEntry creado', () async {
    final fake = _FakeFilesRemoteDataSource()..uploadResult = testFile;
    final repo = FilesRepositoryImpl(remoteDataSource: fake);

    final result = await repo.uploadFile(
      parentPath: '/',
      localFilePath: '/tmp/foto.jpg',
      fileName: 'foto.jpg',
    );

    expect(result, testFile);
    expect(fake.uploadedFileNames, ['foto.jpg']);
  });

  test('createDirectory delega en el data source', () async {
    final fake = _FakeFilesRemoteDataSource();
    final repo = FilesRepositoryImpl(remoteDataSource: fake);

    await repo.createDirectory(parentPath: '/Documentos', name: 'Fotos');

    expect(fake.createdDirectories, ['/Documentos::Fotos']);
  });

  test('una ApiException de uploadFile (p.ej. 409) se propaga sin cambios', () async {
    final fake = _FakeFilesRemoteDataSource()
      ..uploadError = const ApiException(
        code: 'name_occupied_by_trash',
        message: 'x',
      );
    final repo = FilesRepositoryImpl(remoteDataSource: fake);

    await expectLater(
      repo.uploadFile(
        parentPath: '/',
        localFilePath: '/tmp/foto.jpg',
        fileName: 'foto.jpg',
      ),
      throwsA(
        isA<ApiException>()
            .having((e) => e.code, 'code', 'name_occupied_by_trash'),
      ),
    );
  });

  test('downloadFile delega con el archivo y la ruta correctos', () async {
    final fake = _FakeFilesRemoteDataSource();
    final repo = FilesRepositoryImpl(remoteDataSource: fake);

    await repo.downloadFile(file: testFile, saveToPath: '/tmp/out.jpg');

    expect(fake.downloadedFileIds, [testFile.id]);
  });

  test(
    'una ApiException de downloadFile (incluida integrity_mismatch) se propaga',
    () async {
      final fake = _FakeFilesRemoteDataSource()
        ..downloadError = const ApiException(
          code: 'integrity_mismatch',
          message: 'x',
        );
      final repo = FilesRepositoryImpl(remoteDataSource: fake);

      await expectLater(
        repo.downloadFile(file: testFile, saveToPath: '/tmp/out.jpg'),
        throwsA(
          isA<ApiException>()
              .having((e) => e.code, 'code', 'integrity_mismatch'),
        ),
      );
    },
  );

  test('deleteFile delega con permanent=false por defecto', () async {
    final fake = _FakeFilesRemoteDataSource();
    final repo = FilesRepositoryImpl(remoteDataSource: fake);

    await repo.deleteFile('f1');

    expect(fake.deletedFileIds, ['f1']);
    expect(fake.deletedFilePermanentFlags, [false]);
  });

  test('deleteFile delega permanent=true cuando se pide', () async {
    final fake = _FakeFilesRemoteDataSource();
    final repo = FilesRepositoryImpl(remoteDataSource: fake);

    await repo.deleteFile('f1', permanent: true);

    expect(fake.deletedFilePermanentFlags, [true]);
  });

  test('deleteDirectory propaga un error not_empty sin cambios', () async {
    final fake = _FakeFilesRemoteDataSource()
      ..deleteError = const ApiException(code: 'not_empty', message: 'x');
    final repo = FilesRepositoryImpl(remoteDataSource: fake);

    await expectLater(
      repo.deleteDirectory('d1'),
      throwsA(isA<ApiException>().having((e) => e.code, 'code', 'not_empty')),
    );
  });

  test('restoreFile y restoreDirectory delegan correctamente', () async {
    final fake = _FakeFilesRemoteDataSource();
    final repo = FilesRepositoryImpl(remoteDataSource: fake);

    await repo.restoreFile('f1');
    await repo.restoreDirectory('d1');

    expect(fake.restoredFileIds, ['f1']);
    expect(fake.restoredDirectoryIds, ['d1']);
  });

  test('listTrash delega y devuelve tal cual el resultado', () async {
    final fake = _FakeFilesRemoteDataSource()
      ..trashListing = DirectoryListing(directories: const [], files: [testFile]);
    final repo = FilesRepositoryImpl(remoteDataSource: fake);

    final result = await repo.listTrash();

    expect(result.files, [testFile]);
  });

  final testVersion = FileVersion(
    versionNum: 3,
    sizeBytes: 512,
    sha256: 'def456',
    mimeType: 'image/jpeg',
    createdAt: DateTime.utc(2026),
  );

  test('listVersions delega y devuelve tal cual el resultado', () async {
    final fake = _FakeFilesRemoteDataSource()..versions = [testVersion];
    final repo = FilesRepositoryImpl(remoteDataSource: fake);

    final result = await repo.listVersions('f1');

    expect(result, [testVersion]);
  });

  test('una ApiException de listVersions se propaga sin cambios', () async {
    final fake = _FakeFilesRemoteDataSource()
      ..versionsError = const ApiException(code: 'forbidden', message: 'x');
    final repo = FilesRepositoryImpl(remoteDataSource: fake);

    await expectLater(
      repo.listVersions('f1'),
      throwsA(isA<ApiException>().having((e) => e.code, 'code', 'forbidden')),
    );
  });

  test('downloadVersion delega con el fileId y la versión correctos', () async {
    final fake = _FakeFilesRemoteDataSource();
    final repo = FilesRepositoryImpl(remoteDataSource: fake);

    await repo.downloadVersion(
      fileId: 'f1',
      version: testVersion,
      saveToPath: '/tmp/out.jpg',
    );

    expect(fake.downloadedVersionFileIds, ['f1']);
    expect(fake.downloadedVersionNums, [3]);
  });

  test(
    'una ApiException de downloadVersion (incluida integrity_mismatch) se propaga',
    () async {
      final fake = _FakeFilesRemoteDataSource()
        ..downloadVersionError = const ApiException(
          code: 'integrity_mismatch',
          message: 'x',
        );
      final repo = FilesRepositoryImpl(remoteDataSource: fake);

      await expectLater(
        repo.downloadVersion(
          fileId: 'f1',
          version: testVersion,
          saveToPath: '/tmp/out.jpg',
        ),
        throwsA(
          isA<ApiException>()
              .having((e) => e.code, 'code', 'integrity_mismatch'),
        ),
      );
    },
  );

  test(
    'restoreVersion delega con el fileId/versionNum correctos y propaga el FileEntry devuelto',
    () async {
      final fake = _FakeFilesRemoteDataSource()..restoreVersionResult = testFile;
      final repo = FilesRepositoryImpl(remoteDataSource: fake);

      final result = await repo.restoreVersion(fileId: 'f1', versionNum: 3);

      expect(result, testFile);
      expect(fake.restoreVersionFileIds, ['f1']);
      expect(fake.restoreVersionNums, [3]);
    },
  );

  test('una ApiException de restoreVersion se propaga sin cambios', () async {
    final fake = _FakeFilesRemoteDataSource()
      ..restoreVersionError = const ApiException(code: 'not_found', message: 'x');
    final repo = FilesRepositoryImpl(remoteDataSource: fake);

    await expectLater(
      repo.restoreVersion(fileId: 'f1', versionNum: 3),
      throwsA(isA<ApiException>().having((e) => e.code, 'code', 'not_found')),
    );
  });

  test(
    'moveFile delega con fileId/newParentPath/newName correctos y propaga el FileEntry devuelto',
    () async {
      final fake = _FakeFilesRemoteDataSource()..moveFileResult = testFile;
      final repo = FilesRepositoryImpl(remoteDataSource: fake);

      final result = await repo.moveFile('f1', newParentPath: '/Documentos', newName: 'nuevo.jpg');

      expect(result, testFile);
      expect(fake.movedFileIds, ['f1']);
      expect(fake.movedFileNewParentPaths, ['/Documentos']);
      expect(fake.movedFileNewNames, ['nuevo.jpg']);
    },
  );

  test('moveFile delega con newParentPath/newName nulos cuando se omiten', () async {
    final fake = _FakeFilesRemoteDataSource()..moveFileResult = testFile;
    final repo = FilesRepositoryImpl(remoteDataSource: fake);

    await repo.moveFile('f1');

    expect(fake.movedFileNewParentPaths, [null]);
    expect(fake.movedFileNewNames, [null]);
  });

  test('una ApiException de moveFile (p.ej. destination_occupied) se propaga', () async {
    final fake = _FakeFilesRemoteDataSource()
      ..moveFileError = const ApiException(code: 'destination_occupied', message: 'x');
    final repo = FilesRepositoryImpl(remoteDataSource: fake);

    await expectLater(
      repo.moveFile('f1', newName: 'x.jpg'),
      throwsA(isA<ApiException>().having((e) => e.code, 'code', 'destination_occupied')),
    );
  });

  test('moveDirectory delega con el directoryId correcto y propaga el DirectoryEntry devuelto', () async {
    final testDirectory = DirectoryEntry(
      id: 'd1',
      parentPath: '/Archivo',
      name: 'ProyectoViejo',
      createdAt: DateTime.utc(2026),
    );
    final fake = _FakeFilesRemoteDataSource()..moveDirectoryResult = testDirectory;
    final repo = FilesRepositoryImpl(remoteDataSource: fake);

    final result = await repo.moveDirectory('d1', newParentPath: '/Archivo', newName: 'ProyectoViejo');

    expect(result, testDirectory);
    expect(fake.movedDirectoryIds, ['d1']);
  });

  test('una ApiException de moveDirectory (p.ej. invalid_move_destination) se propaga', () async {
    final fake = _FakeFilesRemoteDataSource()
      ..moveDirectoryError = const ApiException(code: 'invalid_move_destination', message: 'x');
    final repo = FilesRepositoryImpl(remoteDataSource: fake);

    await expectLater(
      repo.moveDirectory('d1', newParentPath: '/d1/Sub'),
      throwsA(isA<ApiException>().having((e) => e.code, 'code', 'invalid_move_destination')),
    );
  });
}
