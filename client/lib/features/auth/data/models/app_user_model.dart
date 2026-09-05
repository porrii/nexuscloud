import '../../domain/entities/app_user.dart';

/// Conoce el formato de red (`userResponse` en
/// `internal/api/v1/dto.go`) -- la única capa que lo hace; el dominio no
/// sabe nada de JSON.
class AppUserModel {
  const AppUserModel._();

  static AppUser fromJson(Map<String, dynamic> json) => AppUser(
        id: json['id'] as String,
        username: json['username'] as String,
        displayName:
            (json['display_name'] as String?) ?? json['username'] as String,
        hasTotp: json['has_totp'] as bool? ?? false,
        email: json['email'] as String?,
      );
}
