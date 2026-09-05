import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/di/service_locator.dart';
import 'package:nexuscloud_client/features/auth/domain/entities/app_user.dart';
import 'package:nexuscloud_client/features/auth/domain/entities/auto_login_outcome.dart';
import 'package:nexuscloud_client/features/auth/domain/entities/login_result.dart';
import 'package:nexuscloud_client/features/auth/domain/repositories/auth_repository.dart';
import 'package:nexuscloud_client/features/auth/presentation/pages/login_page.dart';

class _FakeAuthRepository implements AuthRepository {
  LoginResult Function()? onLogin;
  AppUser? _currentUser;

  @override
  AppUser? get currentUser => _currentUser;

  @override
  Stream<AppUser?> get userStream => const Stream.empty();

  @override
  Future<AutoLoginOutcome> tryAutoLogin() async =>
      AutoLoginOutcome.noSavedSession;

  @override
  Future<LoginResult> login({
    required String serverBaseUrl,
    required String username,
    required String password,
    String? totpCode,
  }) async {
    final result = onLogin!();
    if (result is LoginSuccess) _currentUser = result.user;
    return result;
  }

  @override
  Future<void> logout() async {}
}

void main() {
  setUp(() async {
    // GetIt.reset() es async (espera callbacks de disposal) -- sin el
    // await, la limpieza puede completarse DESPUÉS del registro de abajo
    // y borrarlo, dejando `sl<AuthRepository>()` sin nada registrado de
    // forma intermitente.
    await sl.reset();
    sl.registerSingleton<AuthRepository>(_FakeAuthRepository());
  });

  testWidgets('muestra error inline con credenciales incorrectas', (tester) async {
    final fake = sl<AuthRepository>() as _FakeAuthRepository;
    fake.onLogin = () => const LoginFailure(
          LoginFailureReason.invalidCredentials,
          'Usuario o contraseña incorrectos.',
        );

    await tester.pumpWidget(const MaterialApp(home: LoginPage()));
    await _fillAndSubmit(tester);

    expect(find.text('Usuario o contraseña incorrectos.'), findsOneWidget);
    expect(find.text('Código de verificación (TOTP)'), findsNothing);
  });

  testWidgets('revela el campo TOTP cuando el servidor lo exige', (tester) async {
    final fake = sl<AuthRepository>() as _FakeAuthRepository;
    fake.onLogin = () => const LoginFailure(
          LoginFailureReason.totpRequired,
          'Este usuario requiere un código de verificación.',
        );

    await tester.pumpWidget(const MaterialApp(home: LoginPage()));
    await _fillAndSubmit(tester);

    expect(find.text('Código de verificación (TOTP)'), findsOneWidget);
  });
}

Future<void> _fillAndSubmit(WidgetTester tester) async {
  await tester.enterText(
    find.widgetWithText(
      TextFormField,
      'Servidor (p.ej. https://nexuscloud.midominio.com)',
    ),
    'http://server.local',
  );
  await tester.enterText(find.widgetWithText(TextFormField, 'Usuario'), 'ivan');
  await tester.enterText(
    find.widgetWithText(TextFormField, 'Contraseña'),
    'secret',
  );
  await tester.tap(find.text('Entrar'));
  await tester.pumpAndSettle();
}
