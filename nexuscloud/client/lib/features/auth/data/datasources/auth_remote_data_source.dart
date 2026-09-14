import '../../../../core/network/api_client.dart';
import '../../domain/entities/app_user.dart';
import '../models/app_user_model.dart';

/// Llamadas HTTP crudas de auth -- sin lógica de negocio, eso vive en
/// `AuthRepositoryImpl`. Lanza [ApiException] (vía [ApiClient.request]) en
/// cualquier fallo, incluida una contraseña incorrecta.
class AuthRemoteDataSource {
  AuthRemoteDataSource({required ApiClient apiClient}) : _apiClient = apiClient;

  final ApiClient _apiClient;

  Future<LoginResponse> login({
    required String username,
    required String password,
    String? totpCode,
  }) async {
    final response = await _apiClient.request(
      (dio) => dio.post<Map<String, dynamic>>(
        '/auth/login',
        data: {
          'username': username,
          'password': password,
          if (totpCode != null && totpCode.isNotEmpty) 'totp_code': totpCode,
        },
      ),
    );
    final data = response.data!;
    return LoginResponse(
      token: data['token'] as String,
      user: AppUserModel.fromJson(data['user'] as Map<String, dynamic>),
    );
  }

  Future<AppUser> me() async {
    final response = await _apiClient.request(
      (dio) => dio.get<Map<String, dynamic>>('/users/me'),
    );
    return AppUserModel.fromJson(response.data!);
  }

  Future<void> logout() async {
    await _apiClient.request((dio) => dio.post<void>('/auth/logout'));
  }
}

class LoginResponse {
  const LoginResponse({required this.token, required this.user});

  final String token;
  final AppUser user;
}
