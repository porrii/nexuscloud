import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/format/formatters.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../../core/widgets/file_type_icon.dart';
import '../../data/file_content_service.dart';
import '../../domain/entities/file_entry.dart';
import 'preview_bodies.dart';
import 'preview_kind.dart';

/// Abre la vista previa de `files[initialIndex]` (§35). Con varios
/// archivos, se puede pasar al anterior/siguiente sin cerrar -- pensado
/// para recorrer las fotos de una carpeta. [onDownload] descarga el que se
/// está viendo (la vista previa no guarda nada en disco).
Future<void> showFilePreview(
  BuildContext context, {
  required List<FileEntry> files,
  required int initialIndex,
  required ValueChanged<FileEntry> onDownload,
  FileContentService? contentService,
}) {
  return showDialog<void>(
    context: context,
    builder: (context) => FilePreviewDialog(
      files: files,
      initialIndex: initialIndex,
      onDownload: onDownload,
      contentService: contentService,
    ),
  );
}

class FilePreviewDialog extends StatefulWidget {
  const FilePreviewDialog({
    super.key,
    required this.files,
    required this.initialIndex,
    required this.onDownload,
    this.contentService,
  });

  final List<FileEntry> files;
  final int initialIndex;
  final ValueChanged<FileEntry> onDownload;

  /// Por defecto, el registrado en el localizador.
  final FileContentService? contentService;

  @override
  State<FilePreviewDialog> createState() => _FilePreviewDialogState();
}

class _FilePreviewDialogState extends State<FilePreviewDialog> {
  late int _index = widget.initialIndex.clamp(0, widget.files.length - 1);
  late final FileContentService _content =
      widget.contentService ?? sl<FileContentService>();

  FileEntry get _file => widget.files[_index];
  bool get _hasPrevious => _index > 0;
  bool get _hasNext => _index < widget.files.length - 1;

  void _go(int delta) {
    final next = _index + delta;
    if (next < 0 || next >= widget.files.length) return;
    setState(() => _index = next);
  }

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final size = MediaQuery.sizeOf(context);
    final kind = previewKindOf(_file);
    // Las flechas solo cambian de archivo donde no tienen otro uso: el
    // visor de PDF, el texto y el reproductor las usan para desplazarse.
    final arrowsNavigate =
        kind == PreviewKind.image || kind == PreviewKind.unsupported;

    return Dialog(
      insetPadding: const EdgeInsets.all(28),
      clipBehavior: Clip.antiAlias,
      backgroundColor: p.surface,
      child: SizedBox(
        width: size.width - 56,
        height: size.height - 56,
        child: CallbackShortcuts(
          bindings: {
            if (arrowsNavigate) ...{
              const SingleActivator(LogicalKeyboardKey.arrowLeft): () =>
                  _go(-1),
              const SingleActivator(LogicalKeyboardKey.arrowRight): () =>
                  _go(1),
            },
            const SingleActivator(LogicalKeyboardKey.pageUp): () => _go(-1),
            const SingleActivator(LogicalKeyboardKey.pageDown): () => _go(1),
          },
          child: Focus(
            autofocus: true,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                _buildHeader(context),
                Divider(height: 1, color: p.border),
                Expanded(
                  child: PreviewBody(
                    key: ValueKey('preview-${_file.id}'),
                    file: _file,
                    kind: kind,
                    content: _content,
                    onDownload: () => widget.onDownload(_file),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildHeader(BuildContext context) {
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    final file = _file;
    final many = widget.files.length > 1;
    return Padding(
      padding: const EdgeInsets.fromLTRB(18, 10, 10, 10),
      child: Row(
        children: [
          FileTypeIcon.forName(
            file.name,
            mimeType: file.mimeType,
            size: 20,
            boxed: true,
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(
                  file.name,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: text.titleSmall?.copyWith(
                    fontWeight: FontWeight.w600,
                    color: p.textPrimary,
                  ),
                ),
                const SizedBox(height: 2),
                Text(
                  '${formatBytes(file.sizeBytes)} · '
                  '${formatDateTime(file.updatedAt)}',
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: text.bodySmall,
                ),
              ],
            ),
          ),
          if (many) ...[
            Text(
              '${_index + 1} de ${widget.files.length}',
              style: text.bodySmall,
            ),
            const SizedBox(width: 4),
            IconButton(
              tooltip: 'Anterior',
              onPressed: _hasPrevious ? () => _go(-1) : null,
              icon: const Icon(Icons.chevron_left_rounded),
            ),
            IconButton(
              tooltip: 'Siguiente',
              onPressed: _hasNext ? () => _go(1) : null,
              icon: const Icon(Icons.chevron_right_rounded),
            ),
            const SizedBox(width: 4),
          ],
          IconButton(
            tooltip: 'Descargar',
            onPressed: () => widget.onDownload(file),
            icon: const Icon(Icons.download_rounded),
          ),
          IconButton(
            tooltip: 'Cerrar (Esc)',
            onPressed: () => Navigator.of(context).pop(),
            icon: const Icon(Icons.close_rounded),
          ),
        ],
      ),
    );
  }
}
