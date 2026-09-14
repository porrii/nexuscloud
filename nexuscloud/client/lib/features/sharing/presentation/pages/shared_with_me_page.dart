import 'package:file_selector/file_selector.dart';
import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/network/api_exception.dart';
import '../../../files/domain/entities/directory_listing.dart';
import '../../../files/domain/entities/file_entry.dart';
import '../../../files/domain/repositories/files_repository.dart';
import '../../domain/entities/share.dart';
import '../../domain/repositories/sharing_repository.dart';

enum _LoadState { loading, loaded, error }

class _BrowseEntry {
  const _BrowseEntry({required this.id, required this.name});
  final String id;
  final String name;
}

/// Estado efímero de una descarga en curso -- igual criterio que
/// `FileVersionsPage._VersionDownload` (aquí nunca hay subida).
class _SharedDownload {
  double progress = 0;
  String? error;
}

/// "Compartido conmigo" -- calcado de la pestaña homónima de
/// `web/src/pages/SharedPage.tsx`, pero como página propia (no una pestaña
/// de un componente compartido con "Mis comparticiones"), siguiendo el
/// mismo criterio que ya separó `SharePage`/`MySharesPage`.
///
/// Es a la vez la lista plana de comparticiones recibidas Y un
/// mini-explorador de solo lectura para las de tipo carpeta -- navega EN
/// EL PROPIO SITIO (igual que `FileBrowserPage`, que tampoco empuja una
/// página nueva por subcarpeta), nunca por ruta: cada nivel se reautoriza
/// contra el ID real de la (sub)carpeta (ADR-008 §4).
class SharedWithMePage extends StatefulWidget {
  const SharedWithMePage({super.key});

  @override
  State<SharedWithMePage> createState() => _SharedWithMePageState();
}

class _SharedWithMePageState extends State<SharedWithMePage> {
  final SharingRepository _sharingRepository = sl<SharingRepository>();
  final FilesRepository _filesRepository = sl<FilesRepository>();

  _LoadState _state = _LoadState.loading;
  String? _errorMessage;
  List<Share> _shares = [];
  DirectoryListing? _browseListing;
  final List<_BrowseEntry> _browseStack = [];

  // Descargas en curso, en dos mapas separados: el mismo archivo podría
  // aparecer a la vez como compartición directa en la lista plana Y como
  // hijo de una carpeta también compartida (el propietario puede
  // compartir ambas cosas de forma independiente) -- con un único mapa
  // por ID, descargarlo desde los dos sitios pisaría el seguimiento de
  // progreso/error del otro. Como las dos vistas nunca se muestran a la
  // vez (`_isBrowsing` las alterna), separar los mapas no cuesta nada.
  final Map<String, _SharedDownload> _flatDownloads = {};
  final Map<String, _SharedDownload> _browseDownloads = {};

  // Guarda contra respuestas obsoletas: a diferencia de un `_currentPath`
  // escalar (donde como mucho se pisa el contenido un instante),
  // `_browseStack` es una pila -- si se navega dos veces seguidas sobre
  // carpetas HERMANAS antes de que la primera responda, un `push`
  // optimista sin más produciría una migaja que afirma que una está
  // anidada dentro de la otra, y ese error no se autocorrige cuando
  // ambas respuestas por fin llegan. Cada navegación captura su propio
  // valor de generación al entrar y solo aplica su resultado si sigue
  // siendo la generación vigente al recibir la respuesta.
  int _navigationGeneration = 0;

  bool get _isBrowsing => _browseStack.isNotEmpty;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final generation = ++_navigationGeneration;
    setState(() {
      _state = _LoadState.loading;
      _browseStack.clear();
      _browseListing = null;
    });
    try {
      final shares =
          await _sharingRepository.listShares(direction: ShareDirection.withMe);
      if (!mounted || generation != _navigationGeneration) return;
      setState(() {
        _shares = shares;
        _state = _LoadState.loaded;
      });
    } on ApiException catch (e) {
      if (!mounted || generation != _navigationGeneration) return;
      setState(() {
        _errorMessage = e.message;
        _state = _LoadState.error;
      });
    }
  }

  Future<void> _openDirectory(String id, String name) async {
    final generation = ++_navigationGeneration;
    setState(() {
      _state = _LoadState.loading;
      _browseStack.add(_BrowseEntry(id: id, name: name));
    });
    try {
      final listing = await _sharingRepository.listSharedDirectory(id);
      if (!mounted || generation != _navigationGeneration) return;
      setState(() {
        _browseListing = listing;
        _state = _LoadState.loaded;
      });
    } on ApiException catch (e) {
      if (!mounted || generation != _navigationGeneration) return;
      setState(() {
        _errorMessage = e.message;
        _state = _LoadState.error;
      });
    }
  }

  /// `index == -1` vuelve a la raíz -- la única navegación sin llamada de
  /// red, la lista plana ya está en memoria (igual que hace la web).
  Future<void> _navigateToBreadcrumb(int index) async {
    if (index < 0) {
      // Se incrementa igualmente (sin capturarlo) para invalidar cualquier
      // navegación anterior todavía en curso -- esta rama es síncrona, sin
      // `await` después, así que no necesita comprobar su propio valor.
      _navigationGeneration++;
      setState(() {
        _browseStack.clear();
        _browseListing = null;
        _state = _LoadState.loaded;
      });
      return;
    }

    final generation = ++_navigationGeneration;
    setState(() {
      _state = _LoadState.loading;
      _browseStack.removeRange(index + 1, _browseStack.length);
    });
    try {
      final listing =
          await _sharingRepository.listSharedDirectory(_browseStack[index].id);
      if (!mounted || generation != _navigationGeneration) return;
      setState(() {
        _browseListing = listing;
        _state = _LoadState.loaded;
      });
    } on ApiException catch (e) {
      if (!mounted || generation != _navigationGeneration) return;
      setState(() {
        _errorMessage = e.message;
        _state = _LoadState.error;
      });
    }
  }

  Future<void> _refresh() {
    return _isBrowsing ? _navigateToBreadcrumb(_browseStack.length - 1) : _load();
  }

  Future<void> _downloadNestedFile(FileEntry file) async {
    final location = await getSaveLocation(suggestedName: file.name);
    if (location == null || !mounted) return;

    final download = _SharedDownload();
    setState(() => _browseDownloads[file.id] = download);

    try {
      await _filesRepository.downloadFile(
        file: file,
        saveToPath: location.path,
        onProgress: (done, total) {
          if (!mounted || total <= 0) return;
          setState(() => download.progress = done / total);
        },
      );
      if (mounted) setState(() => _browseDownloads.remove(file.id));
    } on ApiException catch (e) {
      if (mounted) setState(() => download.error = e.message);
    }
  }

  Future<void> _downloadFlatShare(Share share) async {
    final location =
        await getSaveLocation(suggestedName: share.resourceName ?? share.resourceId);
    if (location == null || !mounted) return;

    final download = _SharedDownload();
    setState(() => _flatDownloads[share.resourceId] = download);

    try {
      await _sharingRepository.downloadSharedFile(
        fileId: share.resourceId,
        saveToPath: location.path,
        onProgress: (done, total) {
          if (!mounted || total <= 0) return;
          setState(() => download.progress = done / total);
        },
      );
      if (mounted) setState(() => _flatDownloads.remove(share.resourceId));
    } on ApiException catch (e) {
      if (mounted) setState(() => download.error = e.message);
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

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: _isBrowsing ? _buildBreadcrumb() : const Text('Compartido conmigo'),
        actions: [
          IconButton(
            tooltip: 'Actualizar',
            icon: const Icon(Icons.refresh),
            onPressed: _refresh,
          ),
        ],
      ),
      body: _buildBody(),
    );
  }

  Widget _buildBreadcrumb() {
    return SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          TextButton(
            onPressed: () => _navigateToBreadcrumb(-1),
            child: const Text('Compartido conmigo'),
          ),
          for (var i = 0; i < _browseStack.length; i++) ...[
            const Text('/'),
            TextButton(
              onPressed: () => _navigateToBreadcrumb(i),
              child: Text(_browseStack[i].name),
            ),
          ],
        ],
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
                OutlinedButton(onPressed: _refresh, child: const Text('Reintentar')),
              ],
            ),
          ),
        );
      case _LoadState.loaded:
        return _isBrowsing ? _buildBrowseList() : _buildFlatList();
    }
  }

  Widget _buildFlatList() {
    if (_shares.isEmpty) {
      return const Center(child: Text('Nadie ha compartido nada contigo todavía'));
    }
    return ListView(
      children: [
        for (final share in _shares)
          ListTile(
            leading: Icon(
              share.resourceType == ShareResourceType.directory
                  ? Icons.folder
                  : Icons.insert_drive_file,
            ),
            title: Text(share.resourceName ?? share.resourceId),
            subtitle: Text(
              share.resourceType == ShareResourceType.directory
                  ? 'Carpeta compartida'
                  : 'Archivo compartido',
            ),
            onTap: share.resourceType == ShareResourceType.directory
                ? () => _openDirectory(
                      share.resourceId,
                      share.resourceName ?? share.resourceId,
                    )
                : null,
            trailing: share.resourceType == ShareResourceType.file
                ? _buildDownloadTrailing(
                    downloads: _flatDownloads,
                    key: share.resourceId,
                    onDownload: () => _downloadFlatShare(share),
                  )
                : null,
          ),
      ],
    );
  }

  Widget _buildBrowseList() {
    final listing = _browseListing!;
    if (listing.isEmpty) {
      return const Center(child: Text('Esta carpeta está vacía.'));
    }
    return ListView(
      children: [
        for (final directory in listing.directories)
          ListTile(
            leading: const Icon(Icons.folder),
            title: Text(directory.name),
            onTap: () => _openDirectory(directory.id, directory.name),
          ),
        for (final file in listing.files)
          ListTile(
            leading: const Icon(Icons.insert_drive_file),
            title: Text(file.name),
            subtitle: Text(_formatSize(file.sizeBytes)),
            trailing: _buildDownloadTrailing(
              downloads: _browseDownloads,
              key: file.id,
              onDownload: () => _downloadNestedFile(file),
            ),
          ),
      ],
    );
  }

  Widget _buildDownloadTrailing({
    required Map<String, _SharedDownload> downloads,
    required String key,
    required VoidCallback onDownload,
  }) {
    final download = downloads[key];
    if (download == null) {
      return IconButton(
        tooltip: 'Descargar',
        icon: const Icon(Icons.download),
        onPressed: onDownload,
      );
    }
    if (download.error != null) {
      return IconButton(
        tooltip: download.error!,
        icon: Icon(Icons.error_outline, color: Theme.of(context).colorScheme.error),
        onPressed: () => setState(() => downloads.remove(key)),
      );
    }
    return SizedBox(
      width: 32,
      child: LinearProgressIndicator(
        value: download.progress == 0 ? null : download.progress,
      ),
    );
  }
}
