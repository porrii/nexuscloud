import '../../domain/entities/directory_listing.dart';
import '../../domain/repositories/files_repository.dart';
import '../datasources/files_remote_data_source.dart';

class FilesRepositoryImpl implements FilesRepository {
  FilesRepositoryImpl({required FilesRemoteDataSource remoteDataSource})
      : _remoteDataSource = remoteDataSource;

  final FilesRemoteDataSource _remoteDataSource;

  @override
  Future<DirectoryListing> list(String path) => _remoteDataSource.list(path);
}
