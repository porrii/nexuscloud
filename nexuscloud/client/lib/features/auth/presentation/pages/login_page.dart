import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../../core/widgets/view_states.dart';
import '../../../shell/presentation/widgets/sidebar.dart' show BrandMark;
import '../../domain/entities/login_result.dart';
import '../../domain/repositories/auth_repository.dart';

/// Formulario de login. En éxito NO navega por sí misma: actualiza
/// `AuthRepository.userStream`, y es `AuthGatePage` quien reacciona a eso
/// -- el mismo mecanismo que trae de vuelta a esta pantalla ante una
/// caducidad de sesión detectada desde cualquier otro punto de la app.
///
/// En ventanas anchas se parte en dos: un panel de marca a la izquierda y
/// el formulario a la derecha; en estrechas, solo el formulario.
class LoginPage extends StatefulWidget {
  const LoginPage({super.key, this.initialServerUrl, this.infoMessage});

  final String? initialServerUrl;
  final String? infoMessage;

  @override
  State<LoginPage> createState() => _LoginPageState();
}

class _LoginPageState extends State<LoginPage> {
  final _formKey = GlobalKey<FormState>();
  late final _serverUrlController = TextEditingController(
    text: widget.initialServerUrl ?? '',
  );
  final _usernameController = TextEditingController();
  final _passwordController = TextEditingController();
  final _totpController = TextEditingController();

  final AuthRepository _authRepository = sl<AuthRepository>();

  bool _submitting = false;
  bool _showTotpField = false;
  bool _obscurePassword = true;
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
    if (_submitting || !_formKey.currentState!.validate()) return;

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
    final p = context.palette;
    return Scaffold(
      backgroundColor: p.canvas,
      body: LayoutBuilder(
        builder: (context, constraints) {
          final wide = constraints.maxWidth >= 900;
          final form = Center(
            child: SingleChildScrollView(
              padding: const EdgeInsets.all(32),
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 400),
                child: _buildForm(context, showBrand: !wide),
              ),
            ),
          );
          if (!wide) return form;
          return Row(
            children: [
              const Expanded(flex: 5, child: _BrandPanel()),
              Expanded(flex: 6, child: form),
            ],
          );
        },
      ),
    );
  }

  Widget _buildForm(BuildContext context, {required bool showBrand}) {
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    return AutofillGroup(
      child: Form(
        key: _formKey,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            if (showBrand) ...[
              const Center(child: BrandMark(size: 40)),
              const SizedBox(height: 28),
            ],
            Text('Inicia sesión', style: text.headlineSmall),
            const SizedBox(height: 6),
            Text(
              'Conéctate a tu servidor NexusCloud.',
              style: text.bodyMedium?.copyWith(color: p.textMuted),
            ),
            const SizedBox(height: 28),
            if (widget.infoMessage != null) ...[
              InlineAlert(
                message: widget.infoMessage!,
                tone: AlertTone.warning,
              ),
              const SizedBox(height: 16),
            ],
            TextFormField(
              controller: _serverUrlController,
              keyboardType: TextInputType.url,
              textInputAction: TextInputAction.next,
              decoration: const InputDecoration(
                labelText: 'Servidor',
                hintText: 'https://nexuscloud.midominio.com',
                prefixIcon: Icon(Icons.dns_outlined, size: 20),
              ),
              validator: (v) =>
                  (v == null || v.trim().isEmpty) ? 'Obligatorio' : null,
            ),
            const SizedBox(height: 14),
            TextFormField(
              controller: _usernameController,
              autofillHints: const [AutofillHints.username],
              textInputAction: TextInputAction.next,
              decoration: const InputDecoration(
                labelText: 'Usuario',
                prefixIcon: Icon(Icons.person_outline_rounded, size: 20),
              ),
              validator: (v) =>
                  (v == null || v.trim().isEmpty) ? 'Obligatorio' : null,
            ),
            const SizedBox(height: 14),
            TextFormField(
              controller: _passwordController,
              autofillHints: const [AutofillHints.password],
              obscureText: _obscurePassword,
              textInputAction: _showTotpField
                  ? TextInputAction.next
                  : TextInputAction.done,
              onFieldSubmitted: (_) => _showTotpField ? null : _submit(),
              decoration: InputDecoration(
                labelText: 'Contraseña',
                prefixIcon: const Icon(Icons.lock_outline_rounded, size: 20),
                suffixIcon: IconButton(
                  tooltip: _obscurePassword
                      ? 'Mostrar contraseña'
                      : 'Ocultar contraseña',
                  icon: Icon(
                    _obscurePassword
                        ? Icons.visibility_outlined
                        : Icons.visibility_off_outlined,
                    size: 20,
                  ),
                  onPressed: () =>
                      setState(() => _obscurePassword = !_obscurePassword),
                ),
              ),
              validator: (v) => (v == null || v.isEmpty) ? 'Obligatorio' : null,
            ),
            if (_showTotpField) ...[
              const SizedBox(height: 14),
              TextFormField(
                controller: _totpController,
                autofillHints: const [AutofillHints.oneTimeCode],
                keyboardType: TextInputType.number,
                onFieldSubmitted: (_) => _submit(),
                decoration: const InputDecoration(
                  labelText: 'Código de verificación (TOTP)',
                  prefixIcon: Icon(Icons.verified_user_outlined, size: 20),
                  helperText:
                      'El código de 6 cifras de tu app de autenticación.',
                ),
                autofocus: true,
              ),
            ],
            if (_errorMessage != null) ...[
              const SizedBox(height: 16),
              InlineAlert(message: _errorMessage!, tone: AlertTone.danger),
            ],
            const SizedBox(height: 24),
            SizedBox(
              height: 46,
              child: FilledButton(
                onPressed: _submitting ? null : _submit,
                child: _submitting
                    ? const SizedBox(
                        width: 20,
                        height: 20,
                        child: CircularProgressIndicator(
                          strokeWidth: 2,
                          color: Colors.white,
                        ),
                      )
                    : const Text('Entrar'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Panel izquierdo de la pantalla de acceso: degradado de marca, logotipo
/// y tres líneas sobre lo que ofrece la app.
class _BrandPanel extends StatelessWidget {
  const _BrandPanel();

  @override
  Widget build(BuildContext context) {
    Widget feature(IconData icon, String title, String body) => Padding(
      padding: const EdgeInsets.only(bottom: 22),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            width: 38,
            height: 38,
            decoration: BoxDecoration(
              color: Colors.white.withValues(alpha: 0.14),
              borderRadius: BorderRadius.circular(10),
            ),
            child: Icon(icon, color: Colors.white, size: 20),
          ),
          const SizedBox(width: 14),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  title,
                  style: const TextStyle(
                    color: Colors.white,
                    fontWeight: FontWeight.w600,
                    fontSize: 15,
                  ),
                ),
                const SizedBox(height: 2),
                Text(
                  body,
                  style: TextStyle(
                    color: Colors.white.withValues(alpha: 0.78),
                    fontSize: 13.5,
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );

    return Container(
      decoration: const BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [Color(0xFF1D4ED8), Color(0xFF3730A3), Color(0xFF1E1B4B)],
        ),
      ),
      child: Stack(
        children: [
          // Círculos decorativos suaves: dan profundidad sin imágenes.
          Positioned(
            top: -120,
            right: -80,
            child: _Glow(size: 360, opacity: 0.10),
          ),
          Positioned(
            bottom: -140,
            left: -100,
            child: _Glow(size: 420, opacity: 0.07),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(48, 44, 48, 40),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Row(
                  children: [
                    Icon(Icons.cloud_rounded, color: Colors.white, size: 30),
                    SizedBox(width: 10),
                    Text(
                      'NexusCloud',
                      style: TextStyle(
                        color: Colors.white,
                        fontSize: 20,
                        fontWeight: FontWeight.w700,
                        letterSpacing: -0.3,
                      ),
                    ),
                  ],
                ),
                const Spacer(),
                const Text(
                  'Tus archivos,\nen tu servidor.',
                  style: TextStyle(
                    color: Colors.white,
                    fontSize: 34,
                    height: 1.15,
                    fontWeight: FontWeight.w700,
                    letterSpacing: -0.6,
                  ),
                ),
                const SizedBox(height: 32),
                feature(
                  Icons.sync_rounded,
                  'Sincroniza carpetas',
                  'Mantén al día carpetas de este equipo con tu nube, también en segundo plano.',
                ),
                feature(
                  Icons.people_outline_rounded,
                  'Comparte con control',
                  'Con personas, grupos o enlaces con contraseña y caducidad.',
                ),
                feature(
                  Icons.history_rounded,
                  'Nada se pierde',
                  'Historial de versiones y papelera para deshacer cualquier cambio.',
                ),
                const Spacer(),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _Glow extends StatelessWidget {
  const _Glow({required this.size, required this.opacity});

  final double size;
  final double opacity;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        shape: BoxShape.circle,
        gradient: RadialGradient(
          colors: [
            Colors.white.withValues(alpha: opacity),
            Colors.white.withValues(alpha: 0),
          ],
        ),
      ),
    );
  }
}
