import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/format/formatters.dart';
import '../../../../core/network/api_exception.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../../core/widgets/confirm_dialog.dart';
import '../../../../core/widgets/file_type_icon.dart';
import '../../../../core/widgets/page_scaffold.dart';
import '../../../../core/widgets/view_states.dart';
import '../../domain/entities/directory_listing.dart';
import '../../domain/repositories/files_repository.dart';

enum _LoadState { loading, loaded, error }

/// Vista plana de todo lo borrado (no permanentemente) por el usuario --
/// mismas columnas y mismo criterio que `web/src/pages/TrashPage.tsx`:
/// "Restaurar" sin confirmación (reversible), "Eliminar para siempre" con
/// confirmación (irreversible), recarga completa tras cualquier acción
/// (nunca actualización optimista).
class TrashPage extends StatefulWidget {
  const TrashPage({super.key});

  @override
  State<TrashPage> createState() => _TrashPageState();
}

class _TrashPageState extends State<TrashPage> {
  final FilesRepository _filesRepository = sl<FilesRepository>();

  DirectoryListing? _listing;
  _LoadState _state = _LoadState.loading;
  String? _errorMessage;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _state = _LoadState.loading);
    try {
      final listing = await _filesRepository.listTrash();
      if (!mounted) return;
      setState(() {
        _listing = listing;
        _state = _LoadState.loaded;
      });
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() {
        _errorMessage = e.message;
        _state = _LoadState.error;
      });
    }
  }

  Future<void> _run(Future<void> Function() action) async {
    try {
      await action();
    } on ApiException catch (e) {
      if (mounted) {
        ScaffoldMessenger.maybeOf(context)
            ?.showSnackBar(SnackBar(content: Text(e.message)));
      }
    }
    if (mounted) await _load();
  }

  Future<void> _deleteForever({
    required String name,
    required Future<void> Function() action,
  }) async {
    final confirmed = await showConfirmDialog(
      context,
      title: 'Eliminar para siempre',
      message:
          '"$name" se eliminará definitivamente y no podrá recuperarse. '
          '¿Continuar?',
      confirmLabel: 'Eliminar para siempre',
      danger: true,
    );
    if (!confirmed) return;
    await _run(action);
  }

  @override
  Widget build(BuildContext context) {
    final listing = _listing;
    final count = listing == null
        ? 0
        : listing.directories.length + listing.files.length;
    return Scaffold(
      body: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          SubToolbar(
            leading: Text(
              _state == _LoadState.loaded
                  ? '${pluralize(count, 'elemento')} en la papelera del servidor'
                  : 'Papelera del servidor',
              style: Theme.of(context).textTheme.bodyMedium
                  ?.copyWith(color: context.palette.textSecondary),
            ),
            actions: [
              IconButton(
                tooltip: 'Actualizar',
                icon: const Icon(Icons.refresh_rounded),
                onPressed: _load,
              ),
            ],
          ),
          Expanded(child: _buildBody()),
        ],
      ),
    );
  }

  Widget _buildBody() {
    switch (_state) {
      case _LoadState.loading:
        return const LoadingState();
      case _LoadState.error:
        return ErrorState(
          message: _errorMessage ?? 'No se pudo completar la operación.',
          onRetry: _load,
        );
      case _LoadState.loaded:
        final listing = _listing!;
        if (listing.isEmpty) {
          return const EmptyState(
            icon: Icons.delete_outline_rounded,
            title: 'La papelera está vacía',
            message:
                'Lo que borres de «Mis archivos» aparecerá aquí hasta que '
                'se purgue automáticamente.',
          );
        }
        return ListView(
          padding: const EdgeInsets.symmetric(vertical: 8),
          children: [
            for (final directory in listing.directories)
              TrashRow(
                name: directory.name,
                isDirectory: true,
                subtitle:
                    'Ubicación original: ${directory.parentPath} · '
                    'Eliminado: ${formatDateTime(directory.deletedAt!)}',
                onRestore: () =>
                    _run(() => _filesRepository.restoreDirectory(directory.id)),
                onDeleteForever: () => _deleteForever(
                  name: directory.name,
                  action: () => _filesRepository.deleteDirectory(
                    directory.id,
                    permanent: true,
                  ),
                ),
              ),
            for (final file in listing.files)
              TrashRow(
                name: file.name,
                mimeType: file.mimeType,
                subtitle:
                    'Ubicación original: ${file.parentPath} · '
                    'Eliminado: ${formatDateTime(file.deletedAt!)}',
                trailingInfo: formatBytes(file.sizeBytes),
                onRestore: () =>
                    _run(() => _filesRepository.restoreFile(file.id)),
                onDeleteForever: () => _deleteForever(
                  name: file.name,
                  action: () =>
                      _filesRepository.deleteFile(file.id, permanent: true),
                ),
              ),
          ],
        );
    }
  }
}

/// Fila de papelera (servidor o local): icono, nombre, procedencia y las
/// dos acciones. Compartida por `TrashPage` y `LocalTrashPage`.
class TrashRow extends StatelessWidget {
  const TrashRow({
    super.key,
    required this.name,
    required this.subtitle,
    required this.onRestore,
    required this.onDeleteForever,
    this.isDirectory = false,
    this.mimeType,
    this.trailingInfo,
    this.extraLine,
    this.errorMessage,
    this.restoreTooltip = 'Restaurar',
    this.busy = false,
  });

  final String name;
  final String subtitle;
  final String? extraLine;
  final bool isDirectory;
  final String? mimeType;
  final String? trailingInfo;
  final String? errorMessage;
  final String restoreTooltip;
  final bool busy;

  /// `null` deshabilita el botón (p.ej. papelera local sin carpeta).
  final VoidCallback? onRestore;
  final VoidCallback? onDeleteForever;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 2),
      child: DecoratedBox(
        decoration: BoxDecoration(
          border: Border(
            bottom: BorderSide(color: p.border.withValues(alpha: 0.6)),
          ),
        ),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 10),
          child: Row(
            children: [
              Opacity(
                opacity: 0.75,
                child: FileTypeIcon.forName(
                  name,
                  mimeType: mimeType,
                  isDirectory: isDirectory,
                  size: 20,
                  boxed: true,
                ),
              ),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      name,
                      overflow: TextOverflow.ellipsis,
                      style: text.bodyMedium?.copyWith(
                        fontWeight: FontWeight.w500,
                      ),
                    ),
                    const SizedBox(height: 2),
                    Text(
                      subtitle,
                      style: text.bodySmall,
                      overflow: TextOverflow.ellipsis,
                      maxLines: 2,
                    ),
                    if (extraLine != null)
                      Text(
                        extraLine!,
                        style: text.bodySmall,
                        overflow: TextOverflow.ellipsis,
                      ),
                    if (errorMessage != null)
                      Padding(
                        padding: const EdgeInsets.only(top: 2),
                        child: Text(
                          errorMessage!,
                          style: text.bodySmall?.copyWith(color: p.danger),
                        ),
                      ),
                  ],
                ),
              ),
              if (trailingInfo != null) ...[
                const SizedBox(width: 12),
                Text(trailingInfo!, style: text.bodySmall),
              ],
              const SizedBox(width: 12),
              if (busy)
                const Padding(
                  padding: EdgeInsets.symmetric(horizontal: 14),
                  child: SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
                )
              else ...[
                Tooltip(
                  message: restoreTooltip,
                  child: TextButton.icon(
                    onPressed: onRestore,
                    icon: const Icon(Icons.restore_rounded, size: 18),
                    label: const Text('Restaurar'),
                  ),
                ),
                IconButton(
                  tooltip: 'Eliminar para siempre',
                  icon: Icon(
                    Icons.delete_forever_outlined,
                    color: onDeleteForever == null ? null : p.danger,
                  ),
                  onPressed: onDeleteForever,
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}
