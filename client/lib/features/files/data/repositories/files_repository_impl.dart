import '../../domain/entities/directory_listing.dart';
import '../../domain/entities/file_entry.dart';
import '../../domain/repositories/files_repository.dart';
import '../datasources/files_remote_data_source.dart';

class FilesRepositoryImpl implements FilesRepository {
  FilesRepositoryImpl({required FilesRemoteDataSource remoteDataSource})
      : _remoteDataSource = remoteDataSource;

  final FilesRemoteDataSource _remoteDataSource;

  @override
  Future<DirectoryListing> list(String path) => _remoteDataSource.list(path);

  @override
  Future<FileEntry> uploadFile({
    required String parentPath,
    required String localFilePath,
    required String fileName,
    TransferProgress? onProgress,
  }) =>
      _remoteDataSource.uploadFile(
        parentPath: parentPath,
        localFilePath: localFilePath,
        fileName: fileName,
        onProgress: onProgress,
      );

  @override
  Future<void> downloadFile({
    required FileEntry file,
    required String saveToPath,
    TransferProgress? onProgress,
  }) =>
      _remoteDataSource.downloadFile(
        file: file,
        saveToPath: saveToPath,
        onProgress: onProgress,
      );

  @override
  Future<void> deleteFile(String fileId, {bool permanent = false}) =>
      _remoteDataSource.deleteFile(fileId, permanent: permanent);

  @override
  Future<void> deleteDirectory(String directoryId, {bool permanent = false}) =>
      _remoteDataSource.deleteDirectory(directoryId, permanent: permanent);

  @override
  Future<void> restoreFile(String fileId) =>
      _remoteDataSource.restoreFile(fileId);

  @override
  Future<void> restoreDirectory(String directoryId) =>
      _remoteDataSource.restoreDirectory(directoryId);

  @override
  Future<DirectoryListing> listTrash() => _remoteDataSource.listTrash();
}
