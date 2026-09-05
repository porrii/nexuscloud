import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/network/api_client.dart';
import 'package:nexuscloud_client/core/network/api_exception.dart';
import 'package:nexuscloud_client/core/network/session_expiry_notifier.dart';
import 'package:nexuscloud_client/core/storage/server_config_store.dart';
import 'package:nexuscloud_client/core/storage/token_store.dart';
import 'package:nexuscloud_client/features/auth/data/datasources/auth_remote_data_source.dart';
import 'package:nexuscloud_client/features/auth/data/repositories/auth_repository_impl.dart';
import 'package:nexuscloud_client/features/auth/domain/entities/app_user.dart';
import 'package:nexuscloud_client/features/auth/domain/entities/auto_login_outcome.dart';
import 'package:nexuscloud_client/features/auth/domain/entities/login_result.dart';

class _FakeAuthRemoteDataSource implements AuthRemoteDataSource {
  LoginResponse? loginResponse;
  ApiException? loginError;
  AppUser? meResponse;
  ApiException? meError;
  bool logoutShouldThrow = false;
  int logoutCalls = 0;

  @override
  Future<LoginResponse> login({
    required String username,
    required String password,
    String? totpCode,
  }) async {
    if (loginError != null) throw loginError!;
    return loginResponse!;
  }

  @override
  Future<AppUser> me() async {
    if (meError != null) throw meError!;
    return meResponse!;
  }

  @override
  Future<void> logout() async {
    logoutCalls++;
    if (logoutShouldThrow) {
      throw const ApiException(code: 'unauthorized', message: 'x');
    }
  }
}

class _FakeTokenStore implements TokenStore {
  String? token;

  @override
  Future<void> save(String token) async => this.token = token;

  @override
  Future<String?> read() async => token;

  @override
  Future<void> clear() async => token = null;
}

class _FakeServerConfigStore implements ServerConfigStore {
  String? url;

  @override
  Future<void> save(String baseUrl) async => url = baseUrl;

  @override
  Future<String?> read() async => url;

  @override
  Future<void> clear() async => url = null;
}

void main() {
  late _FakeAuthRemoteDataSource remoteDataSource;
  late _FakeTokenStore tokenStore;
  late _FakeServerConfigStore serverConfigStore;
  late SessionExpiryNotifier sessionExpiryNotifier;
  late AuthRepositoryImpl repository;

  const user = AppUser(
    id: 'u1',
    username: 'ivan',
    displayName: 'Ivan',
    hasTotp: false,
  );

  setUp(() {
    remoteDataSource = _FakeAuthRemoteDataSource();
    tokenStore = _FakeTokenStore();
    serverConfigStore = _FakeServerConfigStore();
    sessionExpiryNotifier = SessionExpiryNotifier();
    repository = AuthRepositoryImpl(
      remoteDataSource: remoteDataSource,
      tokenStore: tokenStore,
      serverConfigStore: serverConfigStore,
      // ApiClient real -- en este test nunca llega a hacer una petición
      // HTTP de verdad, AuthRepositoryImpl solo le pide configureBaseUrl.
      apiClient: ApiClient(
        tokenStore: tokenStore,
        sessionExpiryNotifier: sessionExpiryNotifier,
      ),
      sessionExpiryNotifier: sessionExpiryNotifier,
    );
  });

  tearDown(() {
    repository.dispose();
    sessionExpiryNotifier.dispose();
  });

  test('login exitoso guarda el token y publica el usuario', () async {
    remoteDataSource.loginResponse =
        const LoginResponse(token: 'tok-1', user: user);

    final result = await repository.login(
      serverBaseUrl: 'http://server',
      username: 'ivan',
      password: 'secret',
    );

    expect(result, isA<LoginSuccess>());
    expect(await tokenStore.read(), 'tok-1');
    expect(repository.currentUser, user);
    expect(await serverConfigStore.read(), 'http://server');
  });

  test('totp_required no guarda token y expone el motivo', () async {
    remoteDataSource.loginError = const ApiException(
      code: 'totp_required',
      message: 'Falta el código.',
      statusCode: 401,
    );

    final result = await repository.login(
      serverBaseUrl: 'http://server',
      username: 'ivan',
      password: 'secret',
    );

    expect(result, isA<LoginFailure>());
    expect((result as LoginFailure).reason, LoginFailureReason.totpRequired);
    expect(await tokenStore.read(), isNull);
  });

  test('401 confirmado en auto-login limpia el token guardado', () async {
    tokenStore.token = 'stale-token';
    serverConfigStore.url = 'http://server';
    remoteDataSource.meError = const ApiException(
      code: 'unauthorized',
      message: 'x',
      statusCode: 401,
    );

    final outcome = await repository.tryAutoLogin();

    expect(outcome, AutoLoginOutcome.sessionExpired);
    expect(await tokenStore.read(), isNull);
    expect(repository.currentUser, isNull);
  });

  test('un error de red en auto-login NO borra el token guardado', () async {
    tokenStore.token = 'still-good-token';
    serverConfigStore.url = 'http://server';
    remoteDataSource.meError = ApiException.network();

    final outcome = await repository.tryAutoLogin();

    expect(outcome, AutoLoginOutcome.networkError);
    expect(await tokenStore.read(), 'still-good-token');
    expect(repository.currentUser, isNull);
  });

  test('sin token guardado, auto-login es noSavedSession', () async {
    final outcome = await repository.tryAutoLogin();
    expect(outcome, AutoLoginOutcome.noSavedSession);
  });

  test('auto-login exitoso restaura el usuario', () async {
    tokenStore.token = 'good-token';
    serverConfigStore.url = 'http://server';
    remoteDataSource.meResponse = user;

    final outcome = await repository.tryAutoLogin();

    expect(outcome, AutoLoginOutcome.restored);
    expect(repository.currentUser, user);
  });

  test('logout limpia el estado local aunque la llamada remota falle', () async {
    tokenStore.token = 'tok';
    remoteDataSource.logoutShouldThrow = true;

    await repository.logout();

    expect(remoteDataSource.logoutCalls, 1);
    expect(await tokenStore.read(), isNull);
    expect(repository.currentUser, isNull);
  });

  test(
    'un evento de SessionExpiryNotifier limpia sesión sin llamar a logout remoto',
    () async {
      tokenStore.token = 'good-token';
      serverConfigStore.url = 'http://server';
      remoteDataSource.meResponse = user;
      await repository.tryAutoLogin();

      final nextUserEvent = repository.userStream.first;
      sessionExpiryNotifier.notify();
      final emitted = await nextUserEvent;

      expect(emitted, isNull);
      expect(remoteDataSource.logoutCalls, 0);
      expect(await tokenStore.read(), isNull);
    },
  );
}
