import '../entities/app_user.dart';
import '../entities/auto_login_outcome.dart';
import '../entities/login_result.dart';

/// Autenticación contra el servidor NexusCloud configurado.
///
/// Nunca expone el token de sesión -- solo [AppUser]. El token vive
/// exclusivamente en `TokenStore` (core/storage), leído internamente por
/// `ApiClient`.
abstract interface class AuthRepository {
  /// Usuario autenticado actualmente, o null si no hay sesión. Getter
  /// síncrono + [userStream] no-replay -- mismo patrón que
  /// `VaultRepository.currentItems`/`itemsStream` en NexusKeys, elegido
  /// deliberadamente en vez de `flutter_riverpod` (ver ADR-009).
  AppUser? get currentUser;

  Stream<AppUser?> get userStream;

  /// Intenta reanudar una sesión guardada (token + URL de servidor). Se
  /// llama una vez al arrancar la app.
  Future<AutoLoginOutcome> tryAutoLogin();

  Future<LoginResult> login({
    required String serverBaseUrl,
    required String username,
    required String password,
    String? totpCode,
  });

  /// Best-effort: intenta avisar al servidor, pero limpia el estado local
  /// aunque la llamada falle -- una sesión ya caducada en el servidor no
  /// debe bloquear el logout local.
  Future<void> logout();
}
