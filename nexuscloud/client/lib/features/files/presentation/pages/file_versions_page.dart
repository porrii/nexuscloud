import 'package:file_selector/file_selector.dart';
import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/network/api_exception.dart';
import '../../domain/entities/file_entry.dart';
import '../../domain/entities/file_version.dart';
import '../../domain/repositories/files_repository.dart';

enum _LoadState { loading, loaded, error }

/// Estado efímero de una descarga de versión en curso -- igual criterio
/// que `_Transfer` en `file_browser_page.dart`, pero indexado por
/// `versionNum` en vez de mantenerse en una lista, y sin `kind` porque
/// aquí solo se descarga, nunca se sube.
class _VersionDownload {
  double progress = 0;
  String? error;
}

/// Historial de versiones de un archivo (ADR-007) -- calcado de
/// `web/src/components/VersionHistoryDialog.tsx` (misma UX, mismo backend),
/// adaptado a página empujada con `Navigator.push` en vez de a un modal.
/// Restaurar NUNCA pide confirmación (criterio ya establecido en
/// `confirm_dialog.dart`: es no-destructivo, el contenido activo se
/// empuja a su vez al historial antes de traer de vuelta el antiguo).
class FileVersionsPage extends StatefulWidget {
  const FileVersionsPage({super.key, required this.file, this.onRestored});

  final FileEntry file;

  /// Invocado justo tras una restauración exitosa (no al volver atrás),
  /// para que quien empujó esta página (el explorador) pueda refrescar su
  /// propio listado sin depender de un valor de retorno de `Navigator.pop`
  /// ni de que el usuario pulse "Actualizar" a mano.
  final VoidCallback? onRestored;

  @override
  State<FileVersionsPage> createState() => _FileVersionsPageState();
}

class _FileVersionsPageState extends State<FileVersionsPage> {
  final FilesRepository _filesRepository = sl<FilesRepository>();

  late FileEntry _currentFile = widget.file;
  List<FileVersion>? _versions;
  _LoadState _state = _LoadState.loading;
  String? _errorMessage;
  bool _restoring = false;
  final Map<int, _VersionDownload> _downloads = {};

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _state = _LoadState.loading);
    try {
      final versions = await _filesRepository.listVersions(_currentFile.id);
      if (!mounted) return;
      setState(() {
        _versions = versions;
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

  Future<void> _restoreVersion(FileVersion version) async {
    setState(() => _restoring = true);
    try {
      final updated = await _filesRepository.restoreVersion(
        fileId: _currentFile.id,
        versionNum: version.versionNum,
      );
      // Se avisa al explorador incondicionalmente -- pertenece a OTRO
      // widget (posiblemente todavía montado aunque esta página ya no lo
      // esté), así que no debe depender de nuestro propio `mounted`.
      widget.onRestored?.call();
      if (!mounted) return;
      setState(() => _currentFile = updated);
      await _load();
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() {
        _errorMessage = e.message;
        _state = _LoadState.error;
      });
    } finally {
      if (mounted) setState(() => _restoring = false);
    }
  }

  Future<void> _downloadVersion(FileVersion version) async {
    final location = await getSaveLocation(
      suggestedName: _suggestedVersionFileName(_currentFile.name, version.versionNum),
    );
    if (location == null || !mounted) return;

    final download = _VersionDownload();
    setState(() => _downloads[version.versionNum] = download);

    try {
      await _filesRepository.downloadVersion(
        fileId: _currentFile.id,
        version: version,
        saveToPath: location.path,
        onProgress: (done, total) {
          if (!mounted || total <= 0) return;
          setState(() => download.progress = done / total);
        },
      );
      if (mounted) setState(() => _downloads.remove(version.versionNum));
    } on ApiException catch (e) {
      if (mounted) setState(() => download.error = e.message);
    }
  }

  /// Mejora deliberada sobre el nombre genérico `vN` que manda el servidor
  /// en `Content-Disposition`. Un archivo sin extensión real o un
  /// "dotfile" como `.gitignore` (`lastIndexOf('.') <= 0`) no se parte --
  /// se le añade "(vN)" al nombre completo tal cual, para no producir un
  /// resultado peor que el genérico del servidor.
  String _suggestedVersionFileName(String originalName, int versionNum) {
    final dotIndex = originalName.lastIndexOf('.');
    if (dotIndex <= 0) return '$originalName (v$versionNum)';
    final base = originalName.substring(0, dotIndex);
    final ext = originalName.substring(dotIndex);
    return '$base (v$versionNum)$ext';
  }

  String _formatDate(DateTime dateTime) {
    final local = dateTime.toLocal();
    String two(int n) => n.toString().padLeft(2, '0');
    return '${local.year}-${two(local.month)}-${two(local.day)} '
        '${two(local.hour)}:${two(local.minute)}';
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

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            const Text('Historial de versiones', style: TextStyle(fontSize: 16)),
            Text(
              _currentFile.name,
              style: Theme.of(context).textTheme.bodySmall,
            ),
          ],
        ),
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
        final versions = _versions!;
        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text('Versión actual', style: Theme.of(context).textTheme.titleSmall),
                  const SizedBox(height: 4),
                  Text(
                    '${_formatSize(_currentFile.sizeBytes)} · '
                    '${_formatDate(_currentFile.updatedAt)}',
                  ),
                ],
              ),
            ),
            const Divider(height: 1),
            Expanded(
              child: versions.isEmpty
                  ? const Center(
                      child: Text(
                        'Todavía no hay versiones anteriores de este archivo.',
                      ),
                    )
                  : ListView(
                      children: [
                        for (final version in versions)
                          _buildVersionTile(version),
                      ],
                    ),
            ),
          ],
        );
    }
  }

  Widget _buildVersionTile(FileVersion version) {
    final download = _downloads[version.versionNum];
    return ListTile(
      leading: const Icon(Icons.history),
      title: Text('Versión ${version.versionNum}'),
      subtitle: download?.error != null
          ? Text(
              download!.error!,
              style: TextStyle(color: Theme.of(context).colorScheme.error),
            )
          : Text(
              '${_formatSize(version.sizeBytes)} · '
              '${_formatDate(version.createdAt)}',
            ),
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (download != null && download.error == null)
            SizedBox(
              width: 32,
              child: LinearProgressIndicator(
                value: download.progress == 0 ? null : download.progress,
              ),
            )
          else if (download?.error != null)
            IconButton(
              tooltip: 'Descartar',
              icon: const Icon(Icons.close),
              onPressed: () =>
                  setState(() => _downloads.remove(version.versionNum)),
            )
          else
            IconButton(
              tooltip: 'Descargar',
              icon: const Icon(Icons.download),
              onPressed: () => _downloadVersion(version),
            ),
          IconButton(
            tooltip: 'Restaurar',
            icon: const Icon(Icons.restore),
            onPressed: _restoring ? null : () => _restoreVersion(version),
          ),
        ],
      ),
    );
  }
}
