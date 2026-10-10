import 'package:get_it/get_it.dart';

import '../../../core/network/api_client.dart';
import '../data/datasources/files_remote_data_source.dart';
import '../data/browser_preferences.dart';
import '../data/file_content_service.dart';
import '../data/repositories/files_repository_impl.dart';
import '../data/thumbnail_service.dart';
import '../domain/repositories/files_repository.dart';

void configureFilesDependencies(GetIt sl) {
  sl
    ..registerLazySingleton(
      () => FilesRemoteDataSource(apiClient: sl<ApiClient>()),
    )
    ..registerLazySingleton<FilesRepository>(
      () => FilesRepositoryImpl(remoteDataSource: sl()),
    )
    ..registerLazySingleton(() => ThumbnailService(apiClient: sl<ApiClient>()))
    ..registerLazySingleton(BrowserPreferences.new)
    ..registerLazySingleton(
      () => FileContentService(apiClient: sl<ApiClient>()),
    );
}
