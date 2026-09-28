import 'package:flutter/material.dart';
import 'package:path/path.dart' as p;

import '../../../../core/paths/remote_path.dart';
import '../../../../core/theme/app_palette.dart';

enum BrowserViewMode { list, grid }

/// Selector lista / cuadrícula (dos botones de icono en un grupo).
class ViewModeToggle extends StatelessWidget {
  const ViewModeToggle({
    super.key,
    required this.mode,
    required this.onChanged,
  });

  final BrowserViewMode mode;
  final ValueChanged<BrowserViewMode> onChanged;

  @override
  Widget build(BuildContext context) {
    final pal = context.palette;
    Widget button(BrowserViewMode value, IconData icon, String tooltip) {
      final active = value == mode;
      return IconButton(
        tooltip: tooltip,
        isSelected: active,
        onPressed: () => onChanged(value),
        icon: Icon(icon, size: 18),
        style: IconButton.styleFrom(
          foregroundColor: active ? pal.accentOnSoft : pal.textMuted,
          backgroundColor: active ? pal.accentSoft : Colors.transparent,
          minimumSize: const Size(34, 34),
          padding: const EdgeInsets.all(6),
        ),
      );
    }

    return Container(
      padding: const EdgeInsets.all(2),
      decoration: BoxDecoration(
        color: pal.surface,
        borderRadius: BorderRadius.circular(9),
        border: Border.all(color: pal.borderStrong),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          button(
            BrowserViewMode.list,
            Icons.view_list_rounded,
            'Vista de lista',
          ),
          button(
            BrowserViewMode.grid,
            Icons.grid_view_rounded,
            'Vista de cuadrícula',
          ),
        ],
      ),
    );
  }
}

/// Capa que cubre el listado mientras se arrastran archivos desde el
/// Explorador de Windows.
class DropOverlay extends StatelessWidget {
  const DropOverlay({super.key, required this.path});

  final String path;

  @override
  Widget build(BuildContext context) {
    final pal = context.palette;
    final name = path == RemotePath.root
        ? 'Mis archivos'
        : p.posix.basename(path);
    return IgnorePointer(
      child: Container(
        decoration: BoxDecoration(
          color: pal.accentSoft.withValues(alpha: 0.92),
          borderRadius: BorderRadius.circular(12),
          border: Border.all(color: pal.accent, width: 2),
        ),
        child: Center(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(Icons.file_upload_outlined, size: 44, color: pal.accent),
              const SizedBox(height: 12),
              Text(
                'Suelta para subir a «$name»',
                style: TextStyle(
                  color: pal.accentOnSoft,
                  fontSize: 17,
                  fontWeight: FontWeight.w600,
                ),
              ),
              const SizedBox(height: 4),
              Text(
                'Archivos y carpetas completas',
                style: TextStyle(
                  color: pal.accentOnSoft.withValues(alpha: 0.8),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// Entrada de menú contextual con icono, texto y atajo alineado a la
/// derecha (como en los menús nativos de Windows).
PopupMenuItem<T> contextMenuItem<T>(
  T value,
  IconData icon,
  String label, {
  String? shortcut,
  bool danger = false,
}) {
  return PopupMenuItem<T>(
    value: value,
    height: 38,
    child: Builder(
      builder: (context) {
        final pal = context.palette;
        return Row(
          children: [
            Icon(
              icon,
              size: 18,
              color: danger ? pal.danger : pal.textSecondary,
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Text(
                label,
                style: TextStyle(
                  color: danger ? pal.danger : pal.textPrimary,
                  fontSize: 14,
                ),
              ),
            ),
            if (shortcut != null) ...[
              const SizedBox(width: 16),
              Text(
                shortcut,
                style: TextStyle(color: pal.textMuted, fontSize: 12),
              ),
            ],
          ],
        );
      },
    ),
  );
}

/// Barra de selección: "3 seleccionados" y las acciones sobre el lote.
class SelectionBar extends StatelessWidget {
  const SelectionBar({
    super.key,
    required this.count,
    required this.canDownload,
    required this.onDownload,
    required this.onMove,
    required this.onTrash,
    required this.onClear,
  });

  final int count;
  final bool canDownload;
  final VoidCallback onDownload;
  final VoidCallback onMove;
  final VoidCallback onTrash;
  final VoidCallback onClear;

  @override
  Widget build(BuildContext context) {
    final pal = context.palette;
    Widget action(String tooltip, IconData icon, VoidCallback onPressed) =>
        IconButton(
          tooltip: tooltip,
          icon: Icon(icon, color: pal.accentOnSoft, size: 19),
          onPressed: onPressed,
        );
    return Container(
      padding: const EdgeInsets.only(left: 12, right: 4),
      decoration: BoxDecoration(
        color: pal.accentSoft,
        borderRadius: BorderRadius.circular(8),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Flexible(
            child: Text(
              count == 1 ? '1 seleccionado' : '$count seleccionados',
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                color: pal.accentOnSoft,
                fontWeight: FontWeight.w600,
                fontSize: 13,
              ),
            ),
          ),
          const SizedBox(width: 8),
          if (canDownload)
            action('Descargar selección', Icons.download_rounded, onDownload),
          action('Mover selección', Icons.drive_file_move_outline, onMove),
          action(
            'Mover selección a la papelera',
            Icons.delete_outline_rounded,
            onTrash,
          ),
          action('Quitar selección (Esc)', Icons.close_rounded, onClear),
        ],
      ),
    );
  }
}
