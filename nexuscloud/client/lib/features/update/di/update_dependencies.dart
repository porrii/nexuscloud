import 'package:get_it/get_it.dart';

import '../../../core/network/api_client.dart';
import '../data/update_apply_service.dart';
import '../data/update_check_service.dart';

/// La versión actual se inyecta en tiempo de build
/// (`--dart-define=APP_VERSION=...`, ver
/// deploy/scripts/package-client-windows.ps1) -- la misma versión que
/// `vpk pack --packVersion` usa para el paquete que este binario compara
/// contra el feed. En un `flutter run` normal (sin ese define) cae a
/// "0.0.0", que nunca se interpreta como "más reciente" que nada real.
const String appVersion = String.fromEnvironment('APP_VERSION', defaultValue: '0.0.0');

void configureUpdateDependencies(GetIt sl) {
  sl
    ..registerLazySingleton(
      () => UpdateCheckService(apiClient: sl<ApiClient>(), currentVersion: appVersion),
    )
    ..registerLazySingleton(() => UpdateApplyService(apiClient: sl<ApiClient>()));
}
