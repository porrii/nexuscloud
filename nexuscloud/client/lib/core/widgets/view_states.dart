import 'package:flutter/material.dart';

import '../theme/app_palette.dart';

/// Estado vacío: icono en un círculo suave, título, explicación opcional y
/// una acción. Sustituye a los `Center(child: Text('... vacía'))` sueltos.
class EmptyState extends StatelessWidget {
  const EmptyState({
    super.key,
    required this.icon,
    required this.title,
    this.message,
    this.action,
  });

  final IconData icon;
  final String title;
  final String? message;
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    return Center(
      child: SingleChildScrollView(
        padding: const EdgeInsets.all(32),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 380),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Container(
                width: 64,
                height: 64,
                decoration: BoxDecoration(
                  color: p.accentSoft,
                  shape: BoxShape.circle,
                ),
                child: Icon(icon, size: 30, color: p.accent),
              ),
              const SizedBox(height: 16),
              Text(title, style: text.titleMedium, textAlign: TextAlign.center),
              if (message != null) ...[
                const SizedBox(height: 6),
                Text(
                  message!,
                  style: text.bodyMedium?.copyWith(color: p.textMuted),
                  textAlign: TextAlign.center,
                ),
              ],
              if (action != null) ...[const SizedBox(height: 20), action!],
            ],
          ),
        ),
      ),
    );
  }
}

/// Estado de error de carga con botón "Reintentar".
class ErrorState extends StatelessWidget {
  const ErrorState({super.key, required this.message, required this.onRetry});

  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    return Center(
      child: SingleChildScrollView(
        padding: const EdgeInsets.all(32),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 400),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Container(
                width: 64,
                height: 64,
                decoration: BoxDecoration(
                  color: p.dangerSoft,
                  shape: BoxShape.circle,
                ),
                child: Icon(Icons.cloud_off_rounded, size: 30, color: p.danger),
              ),
              const SizedBox(height: 16),
              Text(
                'Algo no ha ido bien',
                style: text.titleMedium,
                textAlign: TextAlign.center,
              ),
              const SizedBox(height: 6),
              Text(
                message,
                style: text.bodyMedium?.copyWith(color: p.textSecondary),
                textAlign: TextAlign.center,
              ),
              const SizedBox(height: 20),
              OutlinedButton.icon(
                onPressed: onRetry,
                icon: const Icon(Icons.refresh_rounded, size: 18),
                label: const Text('Reintentar'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class LoadingState extends StatelessWidget {
  const LoadingState({super.key});

  @override
  Widget build(BuildContext context) {
    return const Center(
      child: SizedBox(
        width: 28,
        height: 28,
        child: CircularProgressIndicator(strokeWidth: 2.6),
      ),
    );
  }
}

enum AlertTone { info, success, warning, danger }

/// Aviso en línea (dentro de un formulario o una tarjeta): fondo suave del
/// color del tono, icono y texto. Para mensajes que no merecen un diálogo.
class InlineAlert extends StatelessWidget {
  const InlineAlert({
    super.key,
    required this.message,
    this.tone = AlertTone.info,
    this.title,
    this.action,
  });

  final String message;
  final String? title;
  final AlertTone tone;
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final (Color fg, Color bg, IconData icon) = switch (tone) {
      AlertTone.info => (
        p.accentOnSoft,
        p.accentSoft,
        Icons.info_outline_rounded,
      ),
      AlertTone.success => (
        p.success,
        p.successSoft,
        Icons.check_circle_outline_rounded,
      ),
      AlertTone.warning => (
        p.warning,
        p.warningSoft,
        Icons.warning_amber_rounded,
      ),
      AlertTone.danger => (p.danger, p.dangerSoft, Icons.error_outline_rounded),
    };
    final text = Theme.of(context).textTheme;
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: bg,
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: fg.withValues(alpha: 0.25)),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, size: 18, color: fg),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (title != null)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 2),
                    child: Text(
                      title!,
                      style: text.titleSmall?.copyWith(color: fg),
                    ),
                  ),
                Text(
                  message,
                  style: text.bodyMedium?.copyWith(
                    color: tone == AlertTone.info ? p.textPrimary : fg,
                    fontSize: 13.5,
                  ),
                ),
                if (action != null) ...[const SizedBox(height: 8), action!],
              ],
            ),
          ),
        ],
      ),
    );
  }
}
