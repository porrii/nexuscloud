import 'package:file_selector/file_selector.dart';
import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/network/api_exception.dart';
import '../../../../core/paths/remote_path.dart';
import '../../../auth/domain/repositories/auth_repository.dart';
import '../../domain/entities/directory_entry.dart';
import '../../domain/entities/directory_listing.dart';
import '../../domain/entities/file_entry.dart';
import '../../domain/repositories/files_repository.dart';
import '../../../sync/presentation/pages/sync_settings_page.dart';
import '../widgets/breadcrumb_bar.dart';

enum _LoadState { loading, loaded, error }

enum _TransferKind { upload, download }

/// Estado efímero de una transferencia en curso -- solo de presentación,
/// nunca persiste ni se comparte con el dominio (a diferencia de
/// [FileEntry]/[DirectoryEntry], que sí vienen del servidor).
class _Transfer {
  _Transfer({required this.kind, required this.name});

  final _TransferKind kind;
  final String name;
  double progress = 0;
  String? error;
}

/// Explorador de archivos -- listado calcado de `web/src/pages/FilesPage.tsx`
/// (mismo backend, misma UX), más subida/descarga (ADR-010). Mismas
/// convenciones que la web: subida secuencial (no en paralelo), el error de
/// una transferencia se muestra en su propia fila (no en un banner global),
/// sin estados "disabled" mientras algo está en curso, sin drag-and-drop.
class FileBrowserPage extends StatefulWidget {
  const FileBrowserPage({super.key});

  @override
  State<FileBrowserPage> createState() => _FileBrowserPageState();
}

class _FileBrowserPageState extends State<FileBrowserPage> {
  final FilesRepository _filesRepository = sl<FilesRepository>();
  final AuthRepository _authRepository = sl<AuthRepository>();

  String _currentPath = RemotePath.root;
  DirectoryListing? _listing;
  _LoadState _state = _LoadState.loading;
  String? _errorMessage;
  final List<_Transfer> _transfers = [];

  @override
  void initState() {
    super.initState();
    _load(_currentPath);
  }

  Future<void> _load(String path) async {
    setState(() {
      _state = _LoadState.loading;
      _currentPath = path;
    });
    try {
      final listing = await _filesRepository.list(path);
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

  void _openDirectory(DirectoryEntry directory) {
    _load(RemotePath.join(_currentPath, directory.name));
  }

  Future<void> _uploadFiles() async {
    final picked = await openFiles();
    if (picked.isEmpty || !mounted) return;

    for (final xfile in picked) {
      final transfer = _Transfer(kind: _TransferKind.upload, name: xfile.name);
      setState(() => _transfers.add(transfer));

      try {
        await _filesRepository.uploadFile(
          parentPath: _currentPath,
          localFilePath: xfile.path,
          fileName: xfile.name,
          onProgress: (done, total) {
            if (!mounted || total <= 0) return;
            setState(() => transfer.progress = done / total);
          },
        );
        if (mounted) setState(() => _transfers.remove(transfer));
      } on ApiException catch (e) {
        if (mounted) setState(() => transfer.error = e.message);
      }
    }

    // Recarga una única vez al final del lote (no tras cada archivo): el
    // listado siempre viene de la verdad del servidor, nunca de una
    // inserción optimista, pero recargar entre archivo y archivo de un
    // mismo lote solo produciría parpadeo del spinner sin aportar nada.
    if (mounted) await _load(_currentPath);
  }

  Future<void> _downloadFile(FileEntry file) async {
    final location = await getSaveLocation(suggestedName: file.name);
    if (location == null || !mounted) return;

    final transfer = _Transfer(kind: _TransferKind.download, name: file.name);
    setState(() => _transfers.add(transfer));

    try {
      await _filesRepository.downloadFile(
        file: file,
        saveToPath: location.path,
        onProgress: (done, total) {
          if (!mounted || total <= 0) return;
          setState(() => transfer.progress = done / total);
        },
      );
      if (mounted) setState(() => _transfers.remove(transfer));
    } on ApiException catch (e) {
      if (mounted) setState(() => transfer.error = e.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: BreadcrumbBar(path: _currentPath, onNavigate: _load),
        actions: [
          IconButton(
            tooltip: 'Sincronización',
            icon: const Icon(Icons.sync),
            onPressed: () => Navigator.of(context).push(
              MaterialPageRoute(builder: (_) => const SyncSettingsPage()),
            ),
          ),
          IconButton(
            tooltip: 'Subir archivo',
            icon: const Icon(Icons.upload_file),
            onPressed: _uploadFiles,
          ),
          IconButton(
            tooltip: 'Actualizar',
            icon: const Icon(Icons.refresh),
            onPressed: () => _load(_currentPath),
          ),
          IconButton(
            tooltip: 'Cerrar sesión',
            icon: const Icon(Icons.logout),
            onPressed: () => _authRepository.logout(),
          ),
        ],
      ),
      body: Column(
        children: [
          if (_transfers.isNotEmpty) _buildTransfersPanel(),
          Expanded(child: _buildBody()),
        ],
      ),
    );
  }

  Widget _buildTransfersPanel() {
    return ConstrainedBox(
      constraints: const BoxConstraints(maxHeight: 160),
      child: DecoratedBox(
        decoration: BoxDecoration(
          border: Border(bottom: BorderSide(color: Theme.of(context).dividerColor)),
        ),
        child: ListView(
          shrinkWrap: true,
          children: [
            for (final transfer in _transfers)
              ListTile(
                dense: true,
                leading: Icon(
                  transfer.kind == _TransferKind.upload
                      ? Icons.upload
                      : Icons.download,
                ),
                title: Text(transfer.name),
                subtitle: transfer.error != null
                    ? Text(
                        transfer.error!,
                        style:
                            TextStyle(color: Theme.of(context).colorScheme.error),
                      )
                    : LinearProgressIndicator(
                        value: transfer.progress == 0 ? null : transfer.progress,
                      ),
                trailing: transfer.error != null
                    ? IconButton(
                        tooltip: 'Descartar',
                        icon: const Icon(Icons.close),
                        onPressed: () =>
                            setState(() => _transfers.remove(transfer)),
                      )
                    : null,
              ),
          ],
        ),
      ),
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
                OutlinedButton(
                  onPressed: () => _load(_currentPath),
                  child: const Text('Reintentar'),
                ),
              ],
            ),
          ),
        );
      case _LoadState.loaded:
        final listing = _listing!;
        if (listing.isEmpty) {
          return const Center(child: Text('Esta carpeta está vacía'));
        }
        return ListView(
          children: [
            for (final directory in listing.directories)
              ListTile(
                leading: const Icon(Icons.folder),
                title: Text(directory.name),
                onTap: () => _openDirectory(directory),
              ),
            for (final file in listing.files)
              ListTile(
                leading: const Icon(Icons.insert_drive_file),
                title: Text(file.name),
                subtitle: Text(_formatSize(file.sizeBytes)),
                trailing: IconButton(
                  tooltip: 'Descargar',
                  icon: const Icon(Icons.download),
                  onPressed: () => _downloadFile(file),
                ),
              ),
          ],
        );
    }
  }

  String _formatSize(int bytes) {
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    var size = bytes.toDouble();
    var unitIndex = 0;
    while (size >= 1024 && unitIndex < units.length - 1) {
      size /= 1024;
      unitIndex++;
    }
    final decimals = (unitIndex == 0 || size >= 10) ? 0 : 1;
    return '${size.toStringAsFixed(decimals)} ${units[unitIndex]}';
  }
}
