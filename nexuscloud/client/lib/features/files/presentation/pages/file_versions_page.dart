import 'package:file_selector/file_selector.dart';
import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/format/formatters.dart';
import '../../../../core/network/api_exception.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../../core/widgets/file_type_icon.dart';
import '../../../../core/widgets/view_states.dart';
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
/// como página completa que el explorador abre en un panel (diálogo
/// grande, `showPanelDialog`), con su botón de cerrar.
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

  @override
  Widget build(BuildContext context) {
    final canPop = Navigator.of(context).canPop();
    return Scaffold(
      appBar: AppBar(
        automaticallyImplyLeading: false,
        toolbarHeight: 64,
        titleSpacing: 20,
        title: Row(
          children: [
            FileTypeIcon.forName(_currentFile.name, mimeType: _currentFile.mimeType, size: 20, boxed: true),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  const Text(
                    'Historial de versiones',
                    style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
                  ),
                  Text(
                    _currentFile.name,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ],
              ),
            ),
          ],
        ),
        actions: [
          IconButton(
            tooltip: 'Actualizar',
            icon: const Icon(Icons.refresh_rounded),
            onPressed: _load,
          ),
          if (canPop)
            IconButton(
              tooltip: 'Cerrar',
              icon: const Icon(Icons.close_rounded),
              onPressed: () => Navigator.of(context).maybePop(),
            ),
          const SizedBox(width: 8),
        ],
      ),
      body: _buildBody(),
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
        final versions = _versions!;
        final p = context.palette;
        final text = Theme.of(context).textTheme;
        return ListView(
          padding: const EdgeInsets.fromLTRB(20, 20, 20, 24),
          children: [
            Container(
              padding: const EdgeInsets.all(16),
              decoration: BoxDecoration(
                color: p.accentSoft,
                borderRadius: BorderRadius.circular(12),
                border: Border.all(color: p.accent.withValues(alpha: 0.25)),
              ),
              child: Row(
                children: [
                  Icon(Icons.verified_outlined, color: p.accent),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text('Versión actual', style: text.titleSmall?.copyWith(color: p.accentOnSoft)),
                        const SizedBox(height: 2),
                        Text(
                          '${formatBytes(_currentFile.sizeBytes)} · '
                          '${formatDateTime(_currentFile.updatedAt)}',
                          style: text.bodyMedium?.copyWith(color: p.textSecondary),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 20),
            Text('VERSIONES ANTERIORES', style: text.labelSmall),
            const SizedBox(height: 8),
            if (versions.isEmpty)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 24),
                child: Text(
                  'Todavía no hay versiones anteriores de este archivo.',
                  textAlign: TextAlign.center,
                  style: text.bodyMedium?.copyWith(color: p.textMuted),
                ),
              )
            else
              Card(
                clipBehavior: Clip.antiAlias,
                child: Column(
                  children: [
                    for (var i = 0; i < versions.length; i++) ...[
                      if (i > 0) const Divider(height: 1),
                      _buildVersionTile(versions[i]),
                    ],
                  ],
                ),
              ),
            const SizedBox(height: 12),
            Text(
              'Restaurar una versión no borra nada: la actual pasa a su vez al historial.',
              style: text.bodySmall,
            ),
          ],
        );
    }
  }

  Widget _buildVersionTile(FileVersion version) {
    final download = _downloads[version.versionNum];
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
      child: Row(
        children: [
          Container(
            width: 36,
            height: 36,
            alignment: Alignment.center,
            decoration: BoxDecoration(color: p.surfaceMuted, borderRadius: BorderRadius.circular(8)),
            child: Text(
              'v${version.versionNum}',
              style: TextStyle(fontSize: 12, fontWeight: FontWeight.w600, color: p.textSecondary),
            ),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text('Versión ${version.versionNum}', style: text.bodyMedium?.copyWith(fontWeight: FontWeight.w500)),
                const SizedBox(height: 2),
                if (download?.error != null)
                  Text(download!.error!, style: text.bodySmall?.copyWith(color: p.danger))
                else
                  Text(
                    '${formatBytes(version.sizeBytes)} · ${formatDateTime(version.createdAt)}',
                    style: text.bodySmall,
                  ),
              ],
            ),
          ),
          if (download != null && download.error == null)
            SizedBox(
              width: 40,
              child: LinearProgressIndicator(
                value: download.progress == 0 ? null : download.progress,
              ),
            )
          else if (download?.error != null)
            IconButton(
              tooltip: 'Descartar',
              icon: const Icon(Icons.close_rounded),
              onPressed: () => setState(() => _downloads.remove(version.versionNum)),
            )
          else
            IconButton(
              tooltip: 'Descargar',
              icon: const Icon(Icons.download_rounded),
              onPressed: () => _downloadVersion(version),
            ),
          const SizedBox(width: 4),
          Tooltip(
            message: 'Restaurar',
            child: OutlinedButton.icon(
              onPressed: _restoring ? null : () => _restoreVersion(version),
              icon: const Icon(Icons.restore_rounded, size: 17),
              label: const Text('Restaurar'),
            ),
          ),
        ],
      ),
    );
  }
}
