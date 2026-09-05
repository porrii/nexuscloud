import 'package:get_it/get_it.dart';

import '../../../core/network/api_client.dart';
import '../../../core/network/session_expiry_notifier.dart';
import '../../../core/storage/server_config_store.dart';
import '../../../core/storage/token_store.dart';
import '../data/datasources/auth_remote_data_source.dart';
import '../data/repositories/auth_repository_impl.dart';
import '../domain/repositories/auth_repository.dart';

void configureAuthDependencies(GetIt sl) {
  sl
    ..registerLazySingleton(
      () => AuthRemoteDataSource(apiClient: sl<ApiClient>()),
    )
    ..registerLazySingleton<AuthRepository>(
      () => AuthRepositoryImpl(
        remoteDataSource: sl(),
        tokenStore: sl<TokenStore>(),
        serverConfigStore: sl<ServerConfigStore>(),
        apiClient: sl<ApiClient>(),
        sessionExpiryNotifier: sl<SessionExpiryNotifier>(),
      ),
    );
}
