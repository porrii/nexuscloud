import 'package:get_it/get_it.dart';

import '../../../core/network/api_client.dart';
import '../data/quota_service.dart';

void configureAccountDependencies(GetIt sl) {
  sl.registerLazySingleton(() => QuotaService(apiClient: sl<ApiClient>()));
}
