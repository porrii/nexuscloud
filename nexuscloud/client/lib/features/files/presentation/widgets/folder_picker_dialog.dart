import 'package:flutter/material.dart';

import '../../../../core/network/api_exception.dart';
import '../../../../core/paths/remote_path.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../../core/widgets/file_type_icon.dart';
import '../../domain/entities/directory_entry.dart';
import '../../domain/repositories/files_repository.dart';
import 'breadcrumb_bar.dart';

/// Elige una carpeta de destino en el servidor ("Mover a…"). Devuelve la
/// ruta elegida o `null` si se cancela. [excludedPaths] son carpetas en
/// las que no se puede entrar ni soltar (las que se están moviendo: el
/// servidor rechaza mover una carpeta dentro de sí misma).
Future<String?> showFolderPickerDialog(
  BuildContext context, {
  required FilesRepository filesRepository,
  required String initialPath,
  required String title,
  String confirmLabel = 'Mover aquí',
  Set<String> excludedPaths = const {},
}) {
  return showDialog<String>(
    context: context,
    builder: (context) => _FolderPickerDialog(
      filesRepository: filesRepository,
      initialPath: initialPath,
      title: title,
      confirmLabel: confirmLabel,
      excludedPaths: excludedPaths,
    ),
  );
}

class _FolderPickerDialog extends StatefulWidget {
  const _FolderPickerDialog({
    required this.filesRepository,
    required this.initialPath,
    required this.title,
    required this.confirmLabel,
    required this.excludedPaths,
  });

  final FilesRepository filesRepository;
  final String initialPath;
  final String title;
  final String confirmLabel;
  final Set<String> excludedPaths;

  @override
  State<_FolderPickerDialog> createState() => _FolderPickerDialogState();
}

class _FolderPickerDialogState extends State<_FolderPickerDialog> {
  late String _path = widget.initialPath;
  List<DirectoryEntry>? _directories;
  String? _error;
  int _generation = 0;

  @override
  void initState() {
    super.initState();
    _load(_path);
  }

  bool _isExcluded(String path) => widget.excludedPaths.any(
    (excluded) => path == excluded || path.startsWith('$excluded/'),
  );

  Future<void> _load(String path) async {
    final generation = ++_generation;
    setState(() {
      _path = path;
      _directories = null;
      _error = null;
    });
    try {
      final listing = await widget.filesRepository.list(path);
      if (!mounted || generation != _generation) return;
      final dirs = [...listing.directories]
        ..sort((a, b) => a.name.toLowerCase().compareTo(b.name.toLowerCase()));
      setState(() => _directories = dirs);
    } on ApiException catch (e) {
      if (!mounted || generation != _generation) return;
      setState(() => _error = e.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    final dirs = _directories;
    final canConfirm = !_isExcluded(_path);

    Widget body;
    if (_error != null) {
      body = Center(
        child: Text(_error!, style: TextStyle(color: p.danger)),
      );
    } else if (dirs == null) {
      body = const Center(child: CircularProgressIndicator(strokeWidth: 2.4));
    } else if (dirs.isEmpty) {
      body = Center(
        child: Text(
          'Sin subcarpetas',
          style: text.bodyMedium?.copyWith(color: p.textMuted),
        ),
      );
    } else {
      body = ListView.builder(
        itemCount: dirs.length,
        itemBuilder: (context, i) {
          final dir = dirs[i];
          final path = RemotePath.join(_path, dir.name);
          final excluded = _isExcluded(path);
          return ListTile(
            dense: true,
            enabled: !excluded,
            leading: const FileTypeIcon(kind: FileKind.folder, size: 20),
            title: Text(dir.name, overflow: TextOverflow.ellipsis),
            trailing: excluded
                ? null
                : const Icon(Icons.chevron_right_rounded, size: 18),
            onTap: excluded ? null : () => _load(path),
          );
        },
      );
    }

    return AlertDialog(
      title: Text(widget.title),
      contentPadding: const EdgeInsets.fromLTRB(16, 12, 16, 0),
      content: SizedBox(
        width: 460,
        height: 360,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            BreadcrumbBar(path: _path, onNavigate: _load, fontSize: 14),
            const SizedBox(height: 8),
            Expanded(
              child: DecoratedBox(
                decoration: BoxDecoration(
                  border: Border.all(color: p.border),
                  borderRadius: BorderRadius.circular(8),
                ),
                child: ClipRRect(
                  borderRadius: BorderRadius.circular(8),
                  child: body,
                ),
              ),
            ),
          ],
        ),
      ),
      actionsPadding: const EdgeInsets.fromLTRB(24, 16, 24, 20),
      actions: [
        OutlinedButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Cancelar'),
        ),
        FilledButton(
          onPressed: canConfirm ? () => Navigator.of(context).pop(_path) : null,
          child: Text(widget.confirmLabel),
        ),
      ],
    );
  }
}
