import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../../../core/network/api_exception.dart';
import '../../../../core/theme/app_palette.dart';

/// Aviso breve al pie (mismo estilo que el explorador).
void showAdminToast(
  BuildContext context,
  String message, {
  bool error = false,
}) {
  final messenger = ScaffoldMessenger.maybeOf(context);
  if (messenger == null) return;
  final p = context.palette;
  messenger
    ..hideCurrentSnackBar()
    ..showSnackBar(
      SnackBar(
        content: Row(
          children: [
            Icon(
              error
                  ? Icons.error_outline_rounded
                  : Icons.check_circle_outline_rounded,
              size: 18,
              color: error ? p.danger : p.success,
            ),
            const SizedBox(width: 10),
            Expanded(child: Text(message)),
          ],
        ),
      ),
    );
}

/// Ejecuta una acción de administración y avisa del resultado. Devuelve
/// `true` si salió bien.
Future<bool> runAdminAction(
  BuildContext context,
  Future<Object?> Function() action, {
  required String success,
}) async {
  try {
    await action();
    if (context.mounted) showAdminToast(context, success);
    return true;
  } on ApiException catch (e) {
    if (context.mounted) showAdminToast(context, e.message, error: true);
    return false;
  }
}

/// Cuánto vive un secreto copiado (contraseña, código de invitación)
/// antes de borrarse del portapapeles.
const Duration kSecretClipboardLifetime = Duration(seconds: 60);

/// Copia [secret] y lo borra del portapapeles pasado
/// [kSecretClipboardLifetime], salvo que para entonces el usuario ya haya
/// copiado otra cosa (no se le pisa). Así una contraseña no se queda
/// indefinidamente al alcance de cualquier aplicación del equipo ni en el
/// historial del portapapeles.
Future<void> copySecretToClipboard(
  BuildContext context,
  String secret, {
  required String what,
}) async {
  await Clipboard.setData(ClipboardData(text: secret));
  if (context.mounted) {
    showAdminToast(
      context,
      '$what copiado. Se borrará del portapapeles en '
      '${kSecretClipboardLifetime.inSeconds} segundos.',
    );
  }
  Timer(kSecretClipboardLifetime, () async {
    final current = await Clipboard.getData(Clipboard.kTextPlain);
    if (current?.text == secret) {
      await Clipboard.setData(const ClipboardData(text: ''));
    }
  });
}
