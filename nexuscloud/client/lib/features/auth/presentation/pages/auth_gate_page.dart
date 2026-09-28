import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/storage/server_config_store.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../shell/presentation/app_shell.dart';
import '../../../shell/presentation/widgets/sidebar.dart' show BrandMark;
import '../../domain/entities/app_user.dart';
import '../../domain/entities/auto_login_outcome.dart';
import '../../domain/repositories/auth_repository.dart';
import 'login_page.dart';

enum _Screen { loading, login, shell }

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

  /// Último servidor usado, para no tener que volver a escribirlo tras
  /// cerrar sesión o si la sesión caduca.
  String? _lastServerUrl;

  @override
  void initState() {
    super.initState();
    _authRepository.userStream.listen(_onUserChanged);
    _bootstrap();
  }

  Future<void> _bootstrap() async {
    if (sl.isRegistered<ServerConfigStore>()) {
      final url = await sl<ServerConfigStore>().read();
      if (mounted && url != null && url.isNotEmpty) {
        setState(() => _lastServerUrl = url);
      }
    }
    final outcome = await _authRepository.tryAutoLogin();
    if (!mounted) return;
    if (outcome == AutoLoginOutcome.networkError) {
      setState(() {
        _infoMessage =
            'No se pudo verificar tu sesión anterior; comprueba '
            'la conexión e inicia sesión de nuevo.';
      });
    }
    // El cambio de pantalla en sí siempre lo dirige userStream
    // (_onUserChanged), sea cual sea el resultado.
  }

  void _onUserChanged(AppUser? user) {
    if (!mounted) return;
    setState(() => _screen = user == null ? _Screen.login : _Screen.shell);
  }

  @override
  Widget build(BuildContext context) {
    final Widget child = switch (_screen) {
      _Screen.loading => const _Splash(),
      _Screen.login => LoginPage(
        key: ValueKey('login-$_lastServerUrl'),
        initialServerUrl: _lastServerUrl,
        infoMessage: _infoMessage,
      ),
      _Screen.shell => const AppShell(),
    };
    return AnimatedSwitcher(
      duration: const Duration(milliseconds: 220),
      child: KeyedSubtree(key: ValueKey(_screen), child: child),
    );
  }
}

class _Splash extends StatelessWidget {
  const _Splash();

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: context.palette.canvas,
      body: const Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            BrandMark(size: 44),
            SizedBox(height: 28),
            SizedBox(
              width: 22,
              height: 22,
              child: CircularProgressIndicator(strokeWidth: 2.4),
            ),
          ],
        ),
      ),
    );
  }
}
