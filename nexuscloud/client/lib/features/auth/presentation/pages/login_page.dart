import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../domain/entities/login_result.dart';
import '../../domain/repositories/auth_repository.dart';

/// Formulario de login. En éxito NO navega por sí misma: actualiza
/// `AuthRepository.userStream`, y es `AuthGatePage` quien reacciona a eso
/// -- el mismo mecanismo que trae de vuelta a esta pantalla ante una
/// caducidad de sesión detectada desde cualquier otro punto de la app.
class LoginPage extends StatefulWidget {
  const LoginPage({super.key, this.initialServerUrl, this.infoMessage});

  final String? initialServerUrl;
  final String? infoMessage;

  @override
  State<LoginPage> createState() => _LoginPageState();
}

class _LoginPageState extends State<LoginPage> {
  final _formKey = GlobalKey<FormState>();
  late final _serverUrlController =
      TextEditingController(text: widget.initialServerUrl ?? '');
  final _usernameController = TextEditingController();
  final _passwordController = TextEditingController();
  final _totpController = TextEditingController();

  final AuthRepository _authRepository = sl<AuthRepository>();

  bool _submitting = false;
  bool _showTotpField = false;
  String? _errorMessage;

  @override
  void dispose() {
    _serverUrlController.dispose();
    _usernameController.dispose();
    _passwordController.dispose();
    _totpController.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (!_formKey.currentState!.validate()) return;

    setState(() {
      _submitting = true;
      _errorMessage = null;
    });

    final result = await _authRepository.login(
      serverBaseUrl: _serverUrlController.text.trim(),
      username: _usernameController.text.trim(),
      password: _passwordController.text,
      totpCode: _totpController.text.trim(),
    );

    if (!mounted) return;

    switch (result) {
      case LoginSuccess():
        break; // AuthGatePage reacciona a userStream; nada más que hacer.
      case LoginFailure(:final reason, :final message):
        setState(() {
          _submitting = false;
          _errorMessage = message;
          if (reason == LoginFailureReason.totpRequired) {
            _showTotpField = true;
          }
        });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Center(
        child: SingleChildScrollView(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 360),
            child: Padding(
              padding: const EdgeInsets.all(24),
              child: Form(
                key: _formKey,
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Text(
                      'NexusCloud',
                      style: Theme.of(context).textTheme.headlineMedium,
                      textAlign: TextAlign.center,
                    ),
                    const SizedBox(height: 24),
                    if (widget.infoMessage != null) ...[
                      Text(
                        widget.infoMessage!,
                        style: TextStyle(
                          color: Theme.of(context).colorScheme.secondary,
                        ),
                      ),
                      const SizedBox(height: 12),
                    ],
                    TextFormField(
                      controller: _serverUrlController,
                      decoration: const InputDecoration(
                        labelText:
                            'Servidor (p.ej. https://nexuscloud.midominio.com)',
                      ),
                      validator: (v) =>
                          (v == null || v.trim().isEmpty) ? 'Obligatorio' : null,
                    ),
                    const SizedBox(height: 12),
                    TextFormField(
                      controller: _usernameController,
                      decoration: const InputDecoration(labelText: 'Usuario'),
                      validator: (v) =>
                          (v == null || v.trim().isEmpty) ? 'Obligatorio' : null,
                    ),
                    const SizedBox(height: 12),
                    TextFormField(
                      controller: _passwordController,
                      decoration: const InputDecoration(labelText: 'Contraseña'),
                      obscureText: true,
                      validator: (v) =>
                          (v == null || v.isEmpty) ? 'Obligatorio' : null,
                    ),
                    if (_showTotpField) ...[
                      const SizedBox(height: 12),
                      TextFormField(
                        controller: _totpController,
                        decoration: const InputDecoration(
                          labelText: 'Código de verificación (TOTP)',
                        ),
                        autofocus: true,
                      ),
                    ],
                    if (_errorMessage != null) ...[
                      const SizedBox(height: 12),
                      Text(
                        _errorMessage!,
                        style:
                            TextStyle(color: Theme.of(context).colorScheme.error),
                      ),
                    ],
                    const SizedBox(height: 24),
                    FilledButton(
                      onPressed: _submitting ? null : _submit,
                      child: _submitting
                          ? const SizedBox(
                              width: 20,
                              height: 20,
                              child: CircularProgressIndicator(strokeWidth: 2),
                            )
                          : const Text('Entrar'),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}
