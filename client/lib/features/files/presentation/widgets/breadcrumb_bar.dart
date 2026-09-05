import 'package:flutter/material.dart';

import '../../../../core/paths/remote_path.dart';

/// Puerto nativo de `web/src/components/Breadcrumbs.tsx`: un chip "Mis
/// archivos" (raíz) más un chip por segmento de [path], cada uno navega a
/// la ruta hasta ese punto.
class BreadcrumbBar extends StatelessWidget {
  const BreadcrumbBar({super.key, required this.path, required this.onNavigate});

  final String path;
  final ValueChanged<String> onNavigate;

  @override
  Widget build(BuildContext context) {
    final segments = RemotePath.segments(path);
    return Wrap(
      crossAxisAlignment: WrapCrossAlignment.center,
      children: [
        _crumb(
          context,
          'Mis archivos',
          RemotePath.root,
          isCurrent: segments.isEmpty,
        ),
        for (var i = 0; i < segments.length; i++) ...[
          const Icon(Icons.chevron_right, size: 18),
          _crumb(
            context,
            segments[i],
            RemotePath.pathUpTo(segments, i),
            isCurrent: i == segments.length - 1,
          ),
        ],
      ],
    );
  }

  Widget _crumb(
    BuildContext context,
    String label,
    String targetPath, {
    required bool isCurrent,
  }) {
    final style = isCurrent
        ? Theme.of(context)
            .textTheme
            .bodyMedium
            ?.copyWith(fontWeight: FontWeight.bold)
        : Theme.of(context).textTheme.bodyMedium;

    if (isCurrent) {
      return Padding(
        padding: const EdgeInsets.symmetric(horizontal: 4),
        child: Text(label, style: style),
      );
    }
    return TextButton(
      style: TextButton.styleFrom(padding: const EdgeInsets.symmetric(horizontal: 4)),
      onPressed: () => onNavigate(targetPath),
      child: Text(label, style: style),
    );
  }
}
