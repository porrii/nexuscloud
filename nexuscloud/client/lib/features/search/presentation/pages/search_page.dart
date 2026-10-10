import 'package:flutter/material.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/format/formatters.dart';
import '../../../../core/network/api_exception.dart';
import '../../../../core/paths/remote_path.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../../core/widgets/file_type_icon.dart';
import '../../../../core/widgets/page_scaffold.dart';
import '../../../../core/widgets/view_states.dart';
import '../../../files/domain/entities/directory_listing.dart';
import '../../data/search_service.dart';

enum _LoadState { loading, loaded, error, disabled }

/// Resultados de la búsqueda global (§33, ADR-040): nombre/ruta en todo el
/// árbol propio, con filtro por tipo. Un clic lleva al explorador a la
/// carpeta del resultado con el elemento ya seleccionado.
class SearchPage extends StatefulWidget {
  const SearchPage({
    super.key,
    required this.initialQuery,
    required this.onOpenLocation,
  });

  final String initialQuery;

  /// `(carpeta, nombre a seleccionar)`.
  final void Function(String path, String? selectName) onOpenLocation;

  @override
  State<SearchPage> createState() => _SearchPageState();
}

class _SearchPageState extends State<SearchPage> {
  final SearchService _searchService = sl<SearchService>();

  SearchKind _kind = SearchKind.all;
  _LoadState _state = _LoadState.loading;
  DirectoryListing? _results;
  String? _error;
  int _generation = 0;

  @override
  void initState() {
    super.initState();
    _run();
  }

  Future<void> _run() async {
    final generation = ++_generation;
    setState(() => _state = _LoadState.loading);
    try {
      final results = await _searchService.search(
        widget.initialQuery,
        kind: _kind,
      );
      if (!mounted || generation != _generation) return;
      setState(() {
        _results = results;
        _state = _LoadState.loaded;
      });
    } on ApiException catch (e) {
      if (!mounted || generation != _generation) return;
      setState(() {
        _state = e.statusCode == 404 ? _LoadState.disabled : _LoadState.error;
        _error = e.message;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final count =
        (_results?.directories.length ?? 0) + (_results?.files.length ?? 0);
    return Scaffold(
      body: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          PageHeader(
            title: 'Resultados para «${widget.initialQuery}»',
            subtitle: _state == _LoadState.loaded
                ? '${pluralize(count, 'resultado')} en todas tus carpetas'
                : 'Buscando por nombre en todas tus carpetas',
            bottom: Wrap(
              spacing: 8,
              children: [
                for (final kind in SearchKind.values)
                  ChoiceChip(
                    label: Text(
                      kind.label,
                      style: TextStyle(
                        color: kind == _kind
                            ? context.palette.accentOnSoft
                            : context.palette.textSecondary,
                        fontWeight: kind == _kind
                            ? FontWeight.w600
                            : FontWeight.w500,
                      ),
                    ),
                    selected: kind == _kind,
                    showCheckmark: false,
                    onSelected: (_) {
                      setState(() => _kind = kind);
                      _run();
                    },
                  ),
              ],
            ),
          ),
          Expanded(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(20, 0, 20, 16),
              child: Card(
                clipBehavior: Clip.antiAlias,
                child: _buildBody(context),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildBody(BuildContext context) {
    switch (_state) {
      case _LoadState.loading:
        return const LoadingState();
      case _LoadState.disabled:
        return const EmptyState(
          icon: Icons.search_off_rounded,
          title: 'La búsqueda no está activada',
          message:
              'El administrador de este servidor la tiene desactivada '
              '(search.enabled). Puedes filtrar dentro de cada carpeta.',
        );
      case _LoadState.error:
        return ErrorState(
          message: _error ?? 'No se pudo buscar.',
          onRetry: _run,
        );
      case _LoadState.loaded:
        final results = _results!;
        if (results.isEmpty) {
          return const EmptyState(
            icon: Icons.manage_search_rounded,
            title: 'Sin resultados',
            message: 'Prueba con otra palabra o quita el filtro de tipo.',
          );
        }
        final rows = <Widget>[
          for (final d in results.directories)
            _ResultRow(
              name: d.name,
              isDirectory: true,
              location: d.parentPath,
              meta: 'Carpeta',
              onTap: () => widget.onOpenLocation(
                RemotePath.join(d.parentPath, d.name),
                null,
              ),
              onReveal: () => widget.onOpenLocation(d.parentPath, d.name),
            ),
          for (final f in results.files)
            _ResultRow(
              name: f.name,
              mimeType: f.mimeType,
              location: f.parentPath,
              meta:
                  '${formatBytes(f.sizeBytes)} · ${formatRelativeDate(f.updatedAt)}',
              onTap: () => widget.onOpenLocation(f.parentPath, f.name),
              onReveal: () => widget.onOpenLocation(f.parentPath, f.name),
            ),
        ];
        return ListView.separated(
          padding: const EdgeInsets.symmetric(vertical: 6),
          itemCount: rows.length,
          separatorBuilder: (_, _) => const SizedBox(height: 0),
          itemBuilder: (_, i) => rows[i],
        );
    }
  }
}

class _ResultRow extends StatelessWidget {
  const _ResultRow({
    required this.name,
    required this.location,
    required this.meta,
    required this.onTap,
    required this.onReveal,
    this.isDirectory = false,
    this.mimeType,
  });

  final String name;
  final String location;
  final String meta;
  final bool isDirectory;
  final String? mimeType;
  final VoidCallback onTap;
  final VoidCallback onReveal;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    final where = [
      'Mis archivos',
      ...RemotePath.segments(location),
    ].join(' › ');
    return InkWell(
      onTap: onTap,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
        child: Row(
          children: [
            FileTypeIcon.forName(
              name,
              mimeType: mimeType,
              isDirectory: isDirectory,
              size: 20,
              boxed: true,
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
                  Row(
                    children: [
                      Icon(Icons.folder_outlined, size: 13, color: p.textMuted),
                      const SizedBox(width: 4),
                      Flexible(
                        child: Text(
                          where,
                          overflow: TextOverflow.ellipsis,
                          style: text.bodySmall,
                        ),
                      ),
                    ],
                  ),
                ],
              ),
            ),
            const SizedBox(width: 12),
            Text(meta, style: text.bodySmall),
            const SizedBox(width: 8),
            IconButton(
              tooltip: 'Mostrar en su carpeta',
              icon: const Icon(Icons.drive_file_move_outline, size: 18),
              onPressed: onReveal,
            ),
          ],
        ),
      ),
    );
  }
}
