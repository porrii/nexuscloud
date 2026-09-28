import 'package:get_it/get_it.dart';

import '../../features/account/data/quota_service.dart';
import '../../features/auth/di/auth_dependencies.dart';
import '../../features/search/data/search_service.dart';
import '../transfers/transfer_queue.dart';
import '../../features/files/di/files_dependencies.dart';
import '../../features/sharing/di/sharing_dependencies.dart';
import '../../features/sync/di/sync_dependencies.dart';
import '../../features/update/di/update_dependencies.dart';
import '../network/api_client.dart';
import '../network/session_expiry_notifier.dart';
import '../storage/local_server_config_store.dart';
import '../storage/local_window_preferences_store.dart';
import '../storage/secure_token_store.dart';
import '../storage/server_config_store.dart';
import '../storage/token_store.dart';
import '../storage/window_preferences_store.dart';
import '../window/app_tray_service.dart';
import '../window/launch_at_startup_service.dart';

/// Localizador de dependencias único de la app, copiando literalmente el
/// patrón de `E:\Proyectos\NexusKeys\NexusKeys\lib\core\di\service_locator.dart`:
/// un `GetIt` global, registros de `core` primero, luego una función
/// `configureXDependencies(sl)` por feature.
final GetIt sl = GetIt.instance;

Future<void> setupServiceLocator() async {
  sl
    ..registerLazySingleton<TokenStore>(SecureTokenStore.new)
    ..registerLazySingleton<ServerConfigStore>(LocalServerConfigStore.new)
    ..registerLazySingleton<WindowPreferencesStore>(
      LocalWindowPreferencesStore.new,
    )
    ..registerLazySingleton(SessionExpiryNotifier.new)
    ..registerLazySingleton(
      () => ApiClient(tokenStore: sl(), sessionExpiryNotifier: sl()),
    )
    ..registerLazySingleton(
      () => AppTrayService(preferencesStore: sl<WindowPreferencesStore>()),
    )
    ..registerLazySingleton(LaunchAtStartupService.new)
    ..registerLazySingleton(TransferQueue.new)
    ..registerLazySingleton(() => QuotaService(apiClient: sl<ApiClient>()))
    ..registerLazySingleton(() => SearchService(apiClient: sl<ApiClient>()));

  configureAuthDependencies(sl);
  configureFilesDependencies(sl);
  configureSharingDependencies(sl);
  configureSyncDependencies(sl);
  configureUpdateDependencies(sl);
}
