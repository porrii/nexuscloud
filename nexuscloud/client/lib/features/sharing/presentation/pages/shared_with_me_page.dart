import 'package:file_selector/file_selector.dart';
import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/format/formatters.dart';
import '../../../../core/network/api_exception.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../../core/transfers/transfer_queue.dart';
import '../../../../core/widgets/file_type_icon.dart';
import '../../../../core/widgets/page_scaffold.dart';
import '../../../../core/widgets/view_states.dart';
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
/// `FileVersionsPage._VersionDownload`.
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
  const SharedWithMePage({super.key, this.transferQueue});

  /// Cola donde se informan las subidas (§37, ADR-035): por defecto la
  /// registrada en el localizador, o una propia si no hay (pruebas).
  final TransferQueue? transferQueue;

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

  // Subidas a la carpeta que se está navegando: van a la cola compartida
  // de transferencias (panel inferior del shell), así que siguen su curso
  // y se ven aunque se cambie de vista mientras tanto.
  late final TransferQueue _transfers;
  TransferQueue? _ownedTransfers;

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
    _transfers = widget.transferQueue ??
        (sl.isRegistered<TransferQueue>()
            ? sl<TransferQueue>()
            : (_ownedTransfers = TransferQueue()));
    _load();
  }

  @override
  void dispose() {
    _ownedTransfers?.dispose();
    super.dispose();
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

  /// Sube a la carpeta que se está navegando ahora mismo (§37, ADR-035) --
  /// solo se llama con el botón visible, que a su vez solo aparece con
  /// `_isBrowsing` y `canUpload` en la carpeta actual (ver `build`). Mismo
  /// patrón que `FileBrowserPage._uploadFiles`: subida secuencial, el error
  /// de una se queda en su propia fila sin abortar las demás, y se
  /// refresca el listado una sola vez al final del lote.
  Future<void> _uploadFiles() async {
    final picked = await openFiles();
    if (picked.isEmpty || !mounted) return;
    final directoryId = _browseStack.last.id;

    final folderName = _browseStack.last.name;

    for (final xfile in picked) {
      final upload = _transfers.start(
        TransferKind.upload,
        xfile.name,
        detail: folderName,
      );
      try {
        await _sharingRepository.uploadToSharedDirectory(
          directoryId: directoryId,
          localFilePath: xfile.path,
          fileName: xfile.name,
          onProgress: (done, total) => _transfers.progress(upload, done, total),
        );
        _transfers.succeed(upload);
      } on ApiException catch (e) {
        _transfers.fail(upload, e.message);
      }
    }

    if (mounted) await _refresh();
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

  @override
  Widget build(BuildContext context) {
    final canUpload = _isBrowsing && (_browseListing?.canUpload ?? false);
    return Scaffold(
      body: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          SubToolbar(
            leading: _buildBreadcrumb(),
            actions: [
              if (canUpload) ...[
                Tooltip(
                  message: 'Subir archivo',
                  child: FilledButton.icon(
                    onPressed: _uploadFiles,
                    icon: const Icon(Icons.upload_rounded, size: 18),
                    label: const Text('Subir aquí'),
                  ),
                ),
                const SizedBox(width: 4),
              ],
              IconButton(
                tooltip: 'Actualizar',
                icon: const Icon(Icons.refresh_rounded),
                onPressed: _refresh,
              ),
            ],
          ),
          Expanded(child: _buildBody()),
        ],
      ),
    );
  }

  Widget _buildBreadcrumb() {
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    Widget crumb(String label, {required bool current, VoidCallback? onTap}) {
      final style = text.bodyMedium?.copyWith(
        fontWeight: current ? FontWeight.w600 : FontWeight.w500,
        color: current ? p.textPrimary : p.textMuted,
      );
      if (current || onTap == null) {
        return Padding(
          padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 4),
          child: Text(label, style: style),
        );
      }
      return InkWell(
        borderRadius: BorderRadius.circular(6),
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 4),
          child: Text(label, style: style),
        ),
      );
    }

    return Align(
      alignment: Alignment.centerLeft,
      child: SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        reverse: true,
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            crumb(
              'Compartido conmigo',
              current: !_isBrowsing,
              onTap: () => _navigateToBreadcrumb(-1),
            ),
            for (var i = 0; i < _browseStack.length; i++) ...[
              Icon(Icons.chevron_right_rounded, size: 18, color: p.textMuted),
              crumb(
                _browseStack[i].name,
                current: i == _browseStack.length - 1,
                onTap: () => _navigateToBreadcrumb(i),
              ),
            ],
          ],
        ),
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
          onRetry: _refresh,
        );
      case _LoadState.loaded:
        return _isBrowsing ? _buildBrowseList() : _buildFlatList();
    }
  }

  Widget _buildFlatList() {
    if (_shares.isEmpty) {
      return const EmptyState(
        icon: Icons.inbox_outlined,
        title: 'Nadie ha compartido nada contigo todavía',
        message: 'Cuando alguien te dé acceso a una carpeta o un archivo, '
            'aparecerá aquí.',
      );
    }
    return ListView(
      padding: const EdgeInsets.symmetric(vertical: 8),
      children: [
        for (final share in _shares)
          _SharedRow(
            name: share.resourceName ?? share.resourceId,
            isDirectory: share.resourceType == ShareResourceType.directory,
            subtitle: share.resourceType == ShareResourceType.directory
                ? 'Carpeta compartida'
                : 'Archivo compartido',
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
                : const Icon(Icons.chevron_right_rounded),
          ),
      ],
    );
  }

  Widget _buildBrowseList() {
    final listing = _browseListing!;
    if (listing.isEmpty) {
      return EmptyState(
        icon: Icons.folder_open_outlined,
        title: 'Esta carpeta está vacía.',
        message: listing.canUpload ? 'Tienes permiso para subir archivos aquí.' : null,
      );
    }
    return ListView(
      padding: const EdgeInsets.symmetric(vertical: 8),
      children: [
        for (final directory in listing.directories)
          _SharedRow(
            name: directory.name,
            isDirectory: true,
            onTap: () => _openDirectory(directory.id, directory.name),
            trailing: const Icon(Icons.chevron_right_rounded),
          ),
        for (final file in listing.files)
          _SharedRow(
            name: file.name,
            mimeType: file.mimeType,
            subtitle: formatBytes(file.sizeBytes),
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
        icon: const Icon(Icons.download_rounded),
        onPressed: onDownload,
      );
    }
    if (download.error != null) {
      return IconButton(
        tooltip: download.error!,
        icon: Icon(Icons.error_outline_rounded, color: context.palette.danger),
        onPressed: () => setState(() => downloads.remove(key)),
      );
    }
    return SizedBox(
      width: 40,
      child: LinearProgressIndicator(
        value: download.progress == 0 ? null : download.progress,
      ),
    );
  }
}

class _SharedRow extends StatelessWidget {
  const _SharedRow({
    required this.name,
    required this.trailing,
    this.subtitle,
    this.isDirectory = false,
    this.mimeType,
    this.onTap,
  });

  final String name;
  final String? subtitle;
  final bool isDirectory;
  final String? mimeType;
  final VoidCallback? onTap;
  final Widget trailing;

  @override
  Widget build(BuildContext context) {
    final text = Theme.of(context).textTheme;
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 1),
      child: InkWell(
        borderRadius: BorderRadius.circular(8),
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
          child: Row(
            children: [
              FileTypeIcon.forName(name, mimeType: mimeType, isDirectory: isDirectory, size: 20, boxed: true),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(name, overflow: TextOverflow.ellipsis, style: text.bodyMedium?.copyWith(fontWeight: FontWeight.w500)),
                    if (subtitle != null) ...[
                      const SizedBox(height: 2),
                      Text(subtitle!, style: text.bodySmall),
                    ],
                  ],
                ),
              ),
              const SizedBox(width: 12),
              trailing,
            ],
          ),
        ),
      ),
    );
  }
}
