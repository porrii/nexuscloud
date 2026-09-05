import 'dart:async';

import '../../../../core/network/api_client.dart';
import '../../../../core/network/api_exception.dart';
import '../../../../core/network/session_expiry_notifier.dart';
import '../../../../core/storage/server_config_store.dart';
import '../../../../core/storage/token_store.dart';
import '../../domain/entities/app_user.dart';
import '../../domain/entities/auto_login_outcome.dart';
import '../../domain/entities/login_result.dart';
import '../../domain/repositories/auth_repository.dart';
import '../datasources/auth_remote_data_source.dart';

class AuthRepositoryImpl implements AuthRepository {
  AuthRepositoryImpl({
    required AuthRemoteDataSource remoteDataSource,
    required TokenStore tokenStore,
    required ServerConfigStore serverConfigStore,
    required ApiClient apiClient,
    required SessionExpiryNotifier sessionExpiryNotifier,
  })  : _remoteDataSource = remoteDataSource,
        _tokenStore = tokenStore,
        _serverConfigStore = serverConfigStore,
        _apiClient = apiClient {
    _sessionExpirySubscription = sessionExpiryNotifier.onSessionExpired
        .listen((_) => _clearSession());
  }

  final AuthRemoteDataSource _remoteDataSource;
  final TokenStore _tokenStore;
  final ServerConfigStore _serverConfigStore;
  final ApiClient _apiClient;

  late final StreamSubscription<void> _sessionExpirySubscription;
  final _userController = StreamController<AppUser?>.broadcast();

  AppUser? _currentUser;

  @override
  AppUser? get currentUser => _currentUser;

  @override
  Stream<AppUser?> get userStream => _userController.stream;

  @override
  Future<AutoLoginOutcome> tryAutoLogin() async {
    final token = await _tokenStore.read();
    final serverUrl = await _serverConfigStore.read();
    if (token == null || serverUrl == null) {
      _setUser(null);
      return AutoLoginOutcome.noSavedSession;
    }

    _apiClient.configureBaseUrl(serverUrl);
    try {
      final user = await _remoteDataSource.me();
      _setUser(user);
      return AutoLoginOutcome.restored;
    } on ApiException catch (e) {
      _setUser(null);
      if (e.statusCode == 401) {
        // Sesión confirmada muerta -- ahora sí se borra.
        await _tokenStore.clear();
        return AutoLoginOutcome.sessionExpired;
      }
      // Error de red u otro fallo del servidor: se conserva el token, no
      // se sabe si la sesión sigue siendo válida (§101).
      return AutoLoginOutcome.networkError;
    }
  }

  @override
  Future<LoginResult> login({
    required String serverBaseUrl,
    required String username,
    required String password,
    String? totpCode,
  }) async {
    _apiClient.configureBaseUrl(serverBaseUrl);
    try {
      final response = await _remoteDataSource.login(
        username: username,
        password: password,
        totpCode: totpCode,
      );
      await _serverConfigStore.save(serverBaseUrl);
      await _tokenStore.save(response.token);
      _setUser(response.user);
      return LoginSuccess(response.user);
    } on ApiException catch (e) {
      return LoginFailure(_reasonFor(e), e.message);
    }
  }

  @override
  Future<void> logout() async {
    try {
      await _remoteDataSource.logout();
    } on ApiException {
      // Best-effort: una sesión ya caducada en el servidor no debe
      // bloquear el logout local.
    }
    await _clearSession();
  }

  Future<void> _clearSession() async {
    await _tokenStore.clear();
    _setUser(null);
  }

  void _setUser(AppUser? user) {
    _currentUser = user;
    _userController.add(user);
  }

  LoginFailureReason _reasonFor(ApiException e) {
    switch (e.code) {
      case 'totp_required':
        return LoginFailureReason.totpRequired;
      case 'totp_invalid':
        return LoginFailureReason.totpInvalid;
      case 'user_disabled':
        return LoginFailureReason.userDisabled;
      case 'unauthorized':
        return LoginFailureReason.invalidCredentials;
      case 'network_error':
        return LoginFailureReason.network;
      default:
        return LoginFailureReason.unknown;
    }
  }

  void dispose() {
    _sessionExpirySubscription.cancel();
    _userController.close();
  }
}
