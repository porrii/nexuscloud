import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/network/api_exception.dart';
import '../../../../core/widgets/confirm_dialog.dart';
import '../../domain/entities/directory_entry.dart';
import '../../domain/entities/directory_listing.dart';
import '../../domain/entities/file_entry.dart';
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

  Future<void> _restoreDirectory(DirectoryEntry directory) async {
    await _filesRepository.restoreDirectory(directory.id);
    if (mounted) await _load();
  }

  Future<void> _restoreFile(FileEntry file) async {
    await _filesRepository.restoreFile(file.id);
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
    await action();
    if (mounted) await _load();
  }

  String _formatDate(DateTime dateTime) {
    final local = dateTime.toLocal();
    String two(int n) => n.toString().padLeft(2, '0');
    return '${local.year}-${two(local.month)}-${two(local.day)} '
        '${two(local.hour)}:${two(local.minute)}';
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Papelera'),
        actions: [
          IconButton(
            tooltip: 'Actualizar',
            icon: const Icon(Icons.refresh),
            onPressed: _load,
          ),
        ],
      ),
      body: _buildBody(),
    );
  }

  Widget _buildBody() {
    switch (_state) {
      case _LoadState.loading:
        return const Center(child: CircularProgressIndicator());
      case _LoadState.error:
        return Center(
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(_errorMessage ?? 'No se pudo completar la operación.'),
                const SizedBox(height: 12),
                OutlinedButton(onPressed: _load, child: const Text('Reintentar')),
              ],
            ),
          ),
        );
      case _LoadState.loaded:
        final listing = _listing!;
        if (listing.isEmpty) {
          return const Center(child: Text('La papelera está vacía'));
        }
        return ListView(
          children: [
            for (final directory in listing.directories)
              ListTile(
                leading: const Icon(Icons.folder),
                title: Text(directory.name),
                subtitle: Text(
                  'Ubicación original: ${directory.parentPath}\n'
                  'Eliminado: ${_formatDate(directory.deletedAt!)}',
                ),
                isThreeLine: true,
                trailing: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    IconButton(
                      tooltip: 'Restaurar',
                      icon: const Icon(Icons.restore),
                      onPressed: () => _restoreDirectory(directory),
                    ),
                    IconButton(
                      tooltip: 'Eliminar para siempre',
                      icon: const Icon(Icons.delete_forever),
                      onPressed: () => _deleteForever(
                        name: directory.name,
                        action: () => _filesRepository.deleteDirectory(
                          directory.id,
                          permanent: true,
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            for (final file in listing.files)
              ListTile(
                leading: const Icon(Icons.insert_drive_file),
                title: Text(file.name),
                subtitle: Text(
                  'Ubicación original: ${file.parentPath}\n'
                  'Eliminado: ${_formatDate(file.deletedAt!)}',
                ),
                isThreeLine: true,
                trailing: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    IconButton(
                      tooltip: 'Restaurar',
                      icon: const Icon(Icons.restore),
                      onPressed: () => _restoreFile(file),
                    ),
                    IconButton(
                      tooltip: 'Eliminar para siempre',
                      icon: const Icon(Icons.delete_forever),
                      onPressed: () => _deleteForever(
                        name: file.name,
                        action: () =>
                            _filesRepository.deleteFile(file.id, permanent: true),
                      ),
                    ),
                  ],
                ),
              ),
          ],
        );
    }
  }
}
