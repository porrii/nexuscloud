import 'package:flutter/material.dart';

/// Diálogo de confirmación genérico, reutilizado por cualquier acción
/// irreversible o semi-irreversible (borrar, eliminar para siempre...) --
/// mismo rol que `ConfirmDialog.tsx` en el cliente web, mismo criterio: no
/// se confirma nada reversible sin coste real (p.ej. "Restaurar" desde la
/// papelera no pasa por aquí).
Future<bool> showConfirmDialog(
  BuildContext context, {
  required String title,
  required String message,
  required String confirmLabel,
  bool danger = false,
}) async {
  final result = await showDialog<bool>(
    context: context,
    builder: (context) => AlertDialog(
      title: Text(title),
      content: Text(message),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(false),
          child: const Text('Cancelar'),
        ),
        FilledButton(
          style: danger
              ? FilledButton.styleFrom(
                  backgroundColor: Theme.of(context).colorScheme.error,
                  foregroundColor: Theme.of(context).colorScheme.onError,
                )
              : null,
          onPressed: () => Navigator.of(context).pop(true),
          child: Text(confirmLabel),
        ),
      ],
    ),
  );
  return result ?? false;
}
