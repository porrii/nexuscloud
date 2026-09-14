import 'package:get_it/get_it.dart';

import '../../../core/network/api_client.dart';
import '../data/datasources/files_remote_data_source.dart';
import '../data/repositories/files_repository_impl.dart';
import '../domain/repositories/files_repository.dart';

void configureFilesDependencies(GetIt sl) {
  sl
    ..registerLazySingleton(
      () => FilesRemoteDataSource(apiClient: sl<ApiClient>()),
    )
    ..registerLazySingleton<FilesRepository>(
      () => FilesRepositoryImpl(remoteDataSource: sl()),
    );
}
