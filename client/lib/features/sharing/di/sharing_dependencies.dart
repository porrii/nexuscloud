import 'package:get_it/get_it.dart';

import '../../../core/network/api_client.dart';
import '../../../core/storage/server_config_store.dart';
import '../data/datasources/sharing_remote_data_source.dart';
import '../data/repositories/sharing_repository_impl.dart';
import '../domain/repositories/sharing_repository.dart';

void configureSharingDependencies(GetIt sl) {
  sl
    ..registerLazySingleton(
      () => SharingRemoteDataSource(apiClient: sl<ApiClient>()),
    )
    ..registerLazySingleton<SharingRepository>(
      () => SharingRepositoryImpl(
        remoteDataSource: sl(),
        serverConfigStore: sl<ServerConfigStore>(),
      ),
    );
}
