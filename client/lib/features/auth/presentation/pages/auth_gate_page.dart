import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../../files/presentation/pages/file_browser_page.dart';
import '../../domain/entities/app_user.dart';
import '../../domain/entities/auto_login_outcome.dart';
import '../../domain/repositories/auth_repository.dart';
import 'login_page.dart';

enum _Screen { loading, login, browser }

/// Pantalla "raíz" que decide qué mostrar según el estado de sesión y
/// reacciona a cambios en cualquier momento (login, logout, caducidad de
/// sesión detectada por `ApiClient`) -- mismo patrón que `AuthGatePage` en
/// NexusKeys: vive para toda la sesión de la app, así que usa un `enum` +
/// `setState` en vez de rutas con nombre, evitando capturas de `context`
/// obsoletas.
class AuthGatePage extends StatefulWidget {
  const AuthGatePage({super.key});

  @override
  State<AuthGatePage> createState() => _AuthGatePageState();
}

class _AuthGatePageState extends State<AuthGatePage> {
  final AuthRepository _authRepository = sl<AuthRepository>();

  _Screen _screen = _Screen.loading;
  String? _infoMessage;

  @override
  void initState() {
    super.initState();
    _authRepository.userStream.listen(_onUserChanged);
    _bootstrap();
  }

  Future<void> _bootstrap() async {
    final outcome = await _authRepository.tryAutoLogin();
    if (!mounted) return;
    if (outcome == AutoLoginOutcome.networkError) {
      setState(() {
        _infoMessage = 'No se pudo verificar tu sesión anterior; comprueba '
            'la conexión e inicia sesión de nuevo.';
      });
    }
    // El cambio de pantalla en sí siempre lo dirige userStream
    // (_onUserChanged), sea cual sea el resultado.
  }

  void _onUserChanged(AppUser? user) {
    if (!mounted) return;
    setState(() => _screen = user == null ? _Screen.login : _Screen.browser);
  }

  @override
  Widget build(BuildContext context) {
    switch (_screen) {
      case _Screen.loading:
        return const Scaffold(
          body: Center(child: CircularProgressIndicator()),
        );
      case _Screen.login:
        return LoginPage(infoMessage: _infoMessage);
      case _Screen.browser:
        return const FileBrowserPage();
    }
  }
}
