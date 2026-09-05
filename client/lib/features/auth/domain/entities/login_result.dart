import 'package:equatable/equatable.dart';

import 'app_user.dart';

/// Resultado de un intento de login: un tipo cerrado que la presentación
/// puede recorrer exhaustivamente con `switch`, en vez de una excepción
/// para lo que casi siempre son resultados ESPERADOS (contraseña
/// incorrecta, hace falta TOTP). Mismo patrón que `AuthResult` en
/// NexusKeys (`lib/features/auth/domain/entities/auth_result.dart`).
sealed class LoginResult extends Equatable {
  const LoginResult();
}

class LoginSuccess extends LoginResult {
  const LoginSuccess(this.user);

  final AppUser user;

  @override
  List<Object?> get props => [user];
}

class LoginFailure extends LoginResult {
  const LoginFailure(this.reason, this.message);

  final LoginFailureReason reason;
  final String message;

  @override
  List<Object?> get props => [reason, message];
}

/// La presentación ramifica sobre esto, nunca sobre `ApiException.message`
/// (§170: texto humano en español, puede cambiar).
enum LoginFailureReason {
  totpRequired,
  totpInvalid,
  userDisabled,
  invalidCredentials,
  network,
  unknown,
}
