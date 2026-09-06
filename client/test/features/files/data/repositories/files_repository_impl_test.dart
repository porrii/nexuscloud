import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/network/api_exception.dart';
import 'package:nexuscloud_client/features/files/data/datasources/files_remote_data_source.dart';
import 'package:nexuscloud_client/features/files/data/repositories/files_repository_impl.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_listing.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_entry.dart';
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
}
