import '../domain/entities/share.dart';

/// Compartido por `SharePage` y `MySharesPage` -- a diferencia de
/// `_formatDate`/`_formatSize` (triviales, cada página tiene su propia
/// copia en el resto del cliente), esta lógica de etiquetas es lo bastante
/// sustancial como para que duplicarla en las dos pantallas de esta misma
/// funcionalidad sea un riesgo real de mantenimiento, no solo repetición
/// cosmética.
///
/// Mismo formato de fecha simple ya usado en
/// `trash_page.dart`/`file_versions_page.dart`.
String formatShareDate(DateTime dateTime) {
  final local = dateTime.toLocal();
  String two(int n) => n.toString().padLeft(2, '0');
  return '${local.year}-${two(local.month)}-${two(local.day)} '
      '${two(local.hour)}:${two(local.minute)}';
}

/// Etiqueta del objetivo de una compartición -- igual criterio que
/// `shareTargetLabel()` en `web/src/components/ShareDialog.tsx`.
String shareTargetLabel(Share share) {
  switch (share.shareType) {
    case ShareType.user:
      return 'Usuario: ${share.targetUsername ?? share.targetUserId}';
    case ShareType.group:
      return 'Grupo: ${share.targetGroupName ?? share.targetGroupId}';
    case ShareType.link:
      final label = share.label;
      return (label != null && label.isNotEmpty) ? label : 'Enlace público';
  }
}

/// Línea de metadatos de una compartición -- igual criterio que la web:
/// permisos, contraseña, expiración, contador de descargas.
String shareMetadataLine(Share share) {
  final parts = <String>[
    share.canUpload ? 'Descarga y subida' : 'Solo descarga',
  ];
  if (share.hasPassword) parts.add('con contraseña');
  if (share.expiresAt != null) {
    parts.add('expira ${formatShareDate(share.expiresAt!)}');
  }
  if (share.maxDownloads != null) {
    parts.add('${share.downloadCount}/${share.maxDownloads} descargas');
  }
  return parts.join(' · ');
}
