import 'package:flutter/material.dart';

import '../theme/app_palette.dart';

/// Diálogo de confirmación genérico, reutilizado por cualquier acción
/// irreversible o semi-irreversible (borrar, eliminar para siempre...) --
/// mismo rol que `ConfirmDialog.tsx` en el cliente web, mismo criterio: no
/// se confirma nada reversible sin coste real (p.ej. "Restaurar" desde la
/// papelera no pasa por aquí).
///
/// El mensaje va en una zona con scroll: la confirmación de borrados de la
/// sincronización puede listar decenas de rutas.
Future<bool> showConfirmDialog(
  BuildContext context, {
  required String title,
  required String message,
  required String confirmLabel,
  bool danger = false,
}) async {
  final result = await showDialog<bool>(
    context: context,
    builder: (context) {
      final p = context.palette;
      final accent = danger ? p.danger : p.accent;
      return AlertDialog(
        icon: Container(
          width: 44,
          height: 44,
          decoration: BoxDecoration(
            color: danger ? p.dangerSoft : p.accentSoft,
            shape: BoxShape.circle,
          ),
          child: Icon(
            danger ? Icons.warning_amber_rounded : Icons.help_outline_rounded,
            color: accent,
            size: 24,
          ),
        ),
        title: Text(title),
        content: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 440, maxHeight: 360),
          child: SingleChildScrollView(child: Text(message)),
        ),
        actionsPadding: const EdgeInsets.fromLTRB(24, 0, 24, 20),
        actions: [
          OutlinedButton(
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
            autofocus: true,
            onPressed: () => Navigator.of(context).pop(true),
            child: Text(confirmLabel),
          ),
        ],
      );
    },
  );
  return result ?? false;
}

/// Muestra una página completa (con su propio `Scaffold`) dentro de un
/// diálogo grande -- así "Compartir" o "Historial de versiones" se abren
/// encima del explorador sin sacar al usuario de la carpeta en la que
/// estaba.
Future<T?> showPanelDialog<T>(
  BuildContext context, {
  required Widget child,
  double width = 600,
  double height = 680,
}) {
  return showDialog<T>(
    context: context,
    builder: (context) {
      final size = MediaQuery.sizeOf(context);
      return Dialog(
        insetPadding: const EdgeInsets.all(24),
        clipBehavior: Clip.antiAlias,
        child: SizedBox(
          width: width.clamp(320, size.width - 48),
          height: height.clamp(360, size.height - 48),
          child: child,
        ),
      );
    },
  );
}

/// Diálogo de un solo campo de texto (nueva carpeta, renombrar). [onSubmit]
/// hace la operación real y devuelve un mensaje de error para mostrarlo
/// bajo el campo, o `null` si salió bien (entonces el diálogo se cierra
/// devolviendo `true`).
Future<bool> showTextInputDialog(
  BuildContext context, {
  required String title,
  required String label,
  required String confirmLabel,
  required Future<String?> Function(String value) onSubmit,
  String initialValue = '',
  IconData icon = Icons.edit_outlined,
  TextSelection? initialSelection,
  String? Function(String value)? validator,
}) async {
  final result = await showDialog<bool>(
    context: context,
    builder: (context) => _TextInputDialog(
      title: title,
      label: label,
      confirmLabel: confirmLabel,
      onSubmit: onSubmit,
      initialValue: initialValue,
      initialSelection: initialSelection,
      icon: icon,
      validator: validator,
    ),
  );
  return result ?? false;
}

class _TextInputDialog extends StatefulWidget {
  const _TextInputDialog({
    required this.title,
    required this.label,
    required this.confirmLabel,
    required this.onSubmit,
    required this.initialValue,
    required this.icon,
    this.initialSelection,
    this.validator,
  });

  final String title;
  final String label;
  final String confirmLabel;
  final Future<String?> Function(String value) onSubmit;
  final String initialValue;
  final TextSelection? initialSelection;
  final IconData icon;
  final String? Function(String value)? validator;

  @override
  State<_TextInputDialog> createState() => _TextInputDialogState();
}

class _TextInputDialogState extends State<_TextInputDialog> {
  late final _controller = TextEditingController(text: widget.initialValue)
    ..selection =
        widget.initialSelection ??
        TextSelection(baseOffset: 0, extentOffset: widget.initialValue.length);
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (_busy) return;
    final value = _controller.text.trim();
    final localError = value.isEmpty
        ? 'Escribe un nombre.'
        : value.contains('/') || value.contains(r'\')
        ? 'El nombre no puede contener "/" ni "\\".'
        : widget.validator?.call(value);
    if (localError != null) {
      setState(() => _error = localError);
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    final error = await widget.onSubmit(value);
    if (!mounted) return;
    if (error == null) {
      Navigator.of(context).pop(true);
    } else {
      setState(() {
        _busy = false;
        _error = error;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return AlertDialog(
      icon: Container(
        width: 44,
        height: 44,
        decoration: BoxDecoration(color: p.accentSoft, shape: BoxShape.circle),
        child: Icon(widget.icon, color: p.accent, size: 22),
      ),
      title: Text(widget.title),
      content: SizedBox(
        width: 400,
        child: TextField(
          controller: _controller,
          autofocus: true,
          enabled: !_busy,
          decoration: InputDecoration(
            labelText: widget.label,
            errorText: _error,
          ),
          onSubmitted: (_) => _submit(),
        ),
      ),
      actionsPadding: const EdgeInsets.fromLTRB(24, 0, 24, 20),
      actions: [
        OutlinedButton(
          onPressed: _busy ? null : () => Navigator.of(context).pop(false),
          child: const Text('Cancelar'),
        ),
        FilledButton(
          onPressed: _busy ? null : _submit,
          child: _busy
              ? const SizedBox(
                  width: 18,
                  height: 18,
                  child: CircularProgressIndicator(
                    strokeWidth: 2,
                    color: Colors.white,
                  ),
                )
              : Text(widget.confirmLabel),
        ),
      ],
    );
  }
}
