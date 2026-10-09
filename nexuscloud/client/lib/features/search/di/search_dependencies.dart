import 'package:get_it/get_it.dart';

import '../../../core/network/api_client.dart';
import '../data/search_service.dart';

void configureSearchDependencies(GetIt sl) {
  sl.registerLazySingleton(() => SearchService(apiClient: sl<ApiClient>()));
}
