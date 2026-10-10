import 'package:get_it/get_it.dart';

import '../../../core/network/api_client.dart';
import '../data/admin_service.dart';

/// Administración (fase A): servicio de datos fino sobre la API, sin
/// dominio/repositorio propio -- excepción consciente a ADR-009, igual que
/// búsqueda y cuota (CRUD directo; la autorización la hace el servidor).
void configureAdminDependencies(GetIt sl) {
  sl.registerLazySingleton(() => AdminService(apiClient: sl<ApiClient>()));
}
