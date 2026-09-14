import 'dart:async';

/// Punto de conexión entre `core/network` y `features/auth` sin invertir la
/// dependencia (ADR-009): la capa de red no conoce `AuthRepository`, solo
/// emite un evento cuando detecta que la sesión ha muerto; quien quiera
/// reaccionar (hoy, `AuthRepositoryImpl`) se suscribe a [onSessionExpired].
class SessionExpiryNotifier {
  final _controller = StreamController<void>.broadcast();

  Stream<void> get onSessionExpired => _controller.stream;

  void notify() => _controller.add(null);

  void dispose() => _controller.close();
}
