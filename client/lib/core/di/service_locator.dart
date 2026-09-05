import 'package:get_it/get_it.dart';

import '../../features/auth/di/auth_dependencies.dart';
import '../../features/files/di/files_dependencies.dart';
import '../network/api_client.dart';
import '../network/session_expiry_notifier.dart';
import '../storage/local_server_config_store.dart';
import '../storage/secure_token_store.dart';
import '../storage/server_config_store.dart';
import '../storage/token_store.dart';

/// Localizador de dependencias único de la app, copiando literalmente el
/// patrón de `E:\Proyectos\NexusKeys\NexusKeys\lib\core\di\service_locator.dart`:
/// un `GetIt` global, registros de `core` primero, luego una función
/// `configureXDependencies(sl)` por feature.
final GetIt sl = GetIt.instance;

Future<void> setupServiceLocator() async {
  sl
    ..registerLazySingleton<TokenStore>(SecureTokenStore.new)
    ..registerLazySingleton<ServerConfigStore>(LocalServerConfigStore.new)
    ..registerLazySingleton(SessionExpiryNotifier.new)
    ..registerLazySingleton(
      () => ApiClient(
        tokenStore: sl(),
        sessionExpiryNotifier: sl(),
      ),
    );

  configureAuthDependencies(sl);
  configureFilesDependencies(sl);
}
