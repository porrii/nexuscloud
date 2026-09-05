import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/network/api_exception.dart';
import '../../../../core/paths/remote_path.dart';
import '../../../auth/domain/repositories/auth_repository.dart';
import '../../domain/entities/directory_entry.dart';
import '../../domain/entities/directory_listing.dart';
import '../../domain/repositories/files_repository.dart';
import '../widgets/breadcrumb_bar.dart';

enum _LoadState { loading, loaded, error }

/// Explorador de archivos de solo lectura -- estados de carga/vacío/error
/// calcados de `web/src/pages/FilesPage.tsx` (mismo backend, misma UX).
/// Sin subida/descarga en este slice.
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

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: BreadcrumbBar(path: _currentPath, onNavigate: _load),
        actions: [
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
