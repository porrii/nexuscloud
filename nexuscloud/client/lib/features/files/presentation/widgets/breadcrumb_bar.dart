import 'package:flutter/material.dart';

import '../../../../core/paths/remote_path.dart';
import '../../../../core/theme/app_palette.dart';

/// Puerto nativo de `web/src/components/Breadcrumbs.tsx`, usado como
/// título del explorador: "Mis archivos › Fotos › 2026", cada tramo
/// navega a la ruta hasta ese punto. Con rutas largas se desplaza en
/// horizontal dejando siempre visible la carpeta actual.
class BreadcrumbBar extends StatelessWidget {
  const BreadcrumbBar({
    super.key,
    required this.path,
    required this.onNavigate,
    this.rootLabel = 'Mis archivos',
    this.fontSize = 20,
  });

  final String path;
  final ValueChanged<String> onNavigate;
  final String rootLabel;
  final double fontSize;

  @override
  Widget build(BuildContext context) {
    final segments = RemotePath.segments(path);
    final p = context.palette;
    // Align + reverse: si la ruta cabe, queda pegada a la izquierda; si no,
    // el scroll arranca por el final para que se vea la carpeta actual.
    return Align(
      alignment: Alignment.centerLeft,
      child: SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        reverse: true,
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            _Crumb(
              label: rootLabel,
              isCurrent: segments.isEmpty,
              fontSize: fontSize,
              onTap: () => onNavigate(RemotePath.root),
            ),
            for (var i = 0; i < segments.length; i++) ...[
              Icon(
                Icons.chevron_right_rounded,
                size: fontSize + 2,
                color: p.textMuted,
              ),
              _Crumb(
                label: segments[i],
                isCurrent: i == segments.length - 1,
                fontSize: fontSize,
                onTap: () => onNavigate(RemotePath.pathUpTo(segments, i)),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _Crumb extends StatelessWidget {
  const _Crumb({
    required this.label,
    required this.isCurrent,
    required this.fontSize,
    required this.onTap,
  });

  final String label;
  final bool isCurrent;
  final double fontSize;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final style = TextStyle(
      fontSize: fontSize,
      fontWeight: FontWeight.w600,
      letterSpacing: -0.1,
      color: isCurrent ? p.textPrimary : p.textMuted,
    );
    if (isCurrent) {
      return Padding(
        padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 4),
        child: Text(label, style: style),
      );
    }
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(6),
      hoverColor: p.surfaceMuted,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 4),
        child: Text(label, style: style),
      ),
    );
  }
}
