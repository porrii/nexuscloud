import 'dart:io';

import 'package:desktop_drop/desktop_drop.dart';
import 'package:file_selector/file_selector.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:path/path.dart' as p;

import '../../../../core/di/service_locator.dart';
import '../../../../core/format/formatters.dart';
import '../../../../core/network/api_exception.dart';
import '../../../../core/paths/remote_path.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../../core/transfers/transfer_queue.dart';
import '../../../../core/widgets/confirm_dialog.dart';
import '../../../../core/widgets/view_states.dart';
import '../../../sharing/domain/entities/share.dart';
import '../../../sharing/presentation/pages/share_page.dart';
import '../../domain/entities/directory_listing.dart';
import '../../domain/entities/file_entry.dart';
import '../../domain/repositories/files_repository.dart';
import '../browser_item.dart';
import '../file_browser_controller.dart';
import '../widgets/breadcrumb_bar.dart';
import '../widgets/browser_chrome.dart';
import '../widgets/file_entry_views.dart';
import '../widgets/folder_picker_dialog.dart';
import 'file_versions_page.dart';

part 'file_browser_actions.dart';

enum _LoadState { loading, loaded, error }

/// Explorador de archivos -- mismo backend y mismas reglas que
/// `web/src/pages/FilesPage.tsx` (subida secuencial, recarga desde el
/// servidor tras cada operación, nunca inserciones optimistas), con la
/// ergonomía de un explorador de escritorio: selección múltiple (Ctrl /
/// Mayús), menú contextual, atajos de teclado, arrastrar y soltar desde el
/// Explorador de Windows, vista de lista o cuadrícula, orden por columna y
/// filtro rápido.
///
/// Las transferencias se informan a la [TransferQueue] compartida (panel
/// inferior del shell) en vez de pintarse encima del listado.
class FileBrowserPage extends StatefulWidget {
  const FileBrowserPage({super.key, this.controller, this.transferQueue});

  final FileBrowserController? controller;

  /// Por defecto, la cola registrada en el localizador; las pruebas que no
  /// la registran reciben una propia de la página.
  final TransferQueue? transferQueue;

  @override
  State<FileBrowserPage> createState() => _FileBrowserPageState();
}

class _FileBrowserPageState extends State<FileBrowserPage>
    with _BrowserActions {
  @override
  final FilesRepository _filesRepository = sl<FilesRepository>();
  @override
  late final TransferQueue _transfers;
  TransferQueue? _ownedTransfers;

  @override
  String _currentPath = RemotePath.root;
  @override
  DirectoryListing? _listing;
  _LoadState _state = _LoadState.loading;
  String? _errorMessage;
  int _loadGeneration = 0;

  SortSpec _sort = const SortSpec(SortField.name);
  BrowserViewMode _viewMode = BrowserViewMode.list;
  final _filterController = TextEditingController();
  final _filterFocus = FocusNode(debugLabel: 'browser-filter');
  final _listFocus = FocusNode(debugLabel: 'browser-list');
  final _scrollController = ScrollController();

  @override
  Set<String> _selected = {};
  @override
  int? _anchorIndex;
  @override
  int? _cursorIndex;
  int _gridColumns = 1;
  bool _dragging = false;

  /// Lista visible (ordenada y filtrada) del último `build` -- la usan los
  /// atajos de teclado y los gestos, que trabajan con índices.
  @override
  List<BrowserItem> _visible = const [];

  @override
  void initState() {
    super.initState();
    _transfers =
        widget.transferQueue ??
        (sl.isRegistered<TransferQueue>()
            ? sl<TransferQueue>()
            : (_ownedTransfers = TransferQueue()));
    final pending = widget.controller?.attach(_openFromController);
    _load(pending?.path ?? RemotePath.root, selectName: pending?.selectName);
  }

  @override
  void dispose() {
    widget.controller?.detach();
    _ownedTransfers?.dispose();
    _filterController.dispose();
    _filterFocus.dispose();
    _listFocus.dispose();
    _scrollController.dispose();
    super.dispose();
  }

  void _openFromController(String path, String? selectName) {
    _load(path, selectName: selectName);
  }

  // -------------------------------------------------------------------------
  // Carga
  // -------------------------------------------------------------------------

  /// Carga [path]. Si es la carpeta actual, conserva la selección de lo que
  /// siga existiendo; si es otra, la limpia junto con el filtro.
  /// [selectName] selecciona y muestra ese elemento al terminar.
  @override
  Future<void> _load(String path, {String? selectName}) async {
    final generation = ++_loadGeneration;
    final samePath = path == _currentPath && _listing != null;
    setState(() {
      _currentPath = path;
      if (!samePath) {
        _state = _LoadState.loading;
        _selected = {};
        _anchorIndex = null;
        _cursorIndex = null;
        _filterController.clear();
      }
    });
    try {
      final listing = await _filesRepository.list(path);
      if (!mounted || generation != _loadGeneration) return;
      setState(() {
        _listing = listing;
        _state = _LoadState.loaded;
        final existing = {
          for (final d in listing.directories) 'd:${d.id}',
          for (final f in listing.files) 'f:${f.id}',
        };
        _selected = _selected.intersection(existing);
      });
      if (selectName != null) _selectByName(selectName);
      if (!samePath && _scrollController.hasClients) {
        _scrollController.jumpTo(0);
      }
    } on ApiException catch (e) {
      if (!mounted || generation != _loadGeneration) return;
      setState(() {
        _errorMessage = e.message;
        _state = _LoadState.error;
      });
    }
  }

  @override
  Future<void> _reload() => _load(_currentPath);

  void _selectByName(String name) {
    final items = _computeVisible();
    final index = items.indexWhere((i) => i.name == name);
    if (index < 0) return;
    setState(() {
      _selected = {items[index].key};
      _anchorIndex = index;
      _cursorIndex = index;
    });
    WidgetsBinding.instance.addPostFrameCallback((_) => _ensureVisible(index));
    _listFocus.requestFocus();
  }

  void _goUp() {
    if (_currentPath == RemotePath.root) return;
    final parent = p.posix.dirname(_currentPath);
    final child = p.posix.basename(_currentPath);
    _load(parent.isEmpty ? RemotePath.root : parent, selectName: child);
  }

  List<BrowserItem> _computeVisible() {
    final listing = _listing;
    if (listing == null) return const [];
    return buildBrowserItems(
      listing,
      sort: _sort,
      filter: _filterController.text,
    );
  }

  @override
  List<BrowserItem> get _selectedItems =>
      _visible.where((i) => _selected.contains(i.key)).toList();

  // -------------------------------------------------------------------------
  // Selección y teclado
  // -------------------------------------------------------------------------

  void _handleSelect(int index) {
    _listFocus.requestFocus();
    final items = _visible;
    if (index >= items.length) return;
    final key = items[index].key;
    final keyboard = HardwareKeyboard.instance;
    setState(() {
      if (keyboard.isShiftPressed && _anchorIndex != null) {
        final range = _rangeKeys(_anchorIndex!, index);
        _selected = keyboard.isControlPressed
            ? {..._selected, ...range}
            : range;
      } else if (keyboard.isControlPressed) {
        _selected = _selected.contains(key)
            ? ({..._selected}..remove(key))
            : {..._selected, key};
        _anchorIndex = index;
      } else {
        _selected = {key};
        _anchorIndex = index;
      }
      _cursorIndex = index;
    });
  }

  Set<String> _rangeKeys(int from, int to) {
    final start = from < to ? from : to;
    final end = from < to ? to : from;
    return {
      for (var i = start; i <= end && i < _visible.length; i++) _visible[i].key,
    };
  }

  void _moveCursor(int delta, {bool extend = false}) {
    final items = _visible;
    if (items.isEmpty) return;
    final current = _cursorIndex ?? (delta > 0 ? -1 : items.length);
    final next = (current + delta).clamp(0, items.length - 1);
    setState(() {
      _cursorIndex = next;
      if (extend && _anchorIndex != null) {
        _selected = _rangeKeys(_anchorIndex!, next);
      } else {
        _selected = {items[next].key};
        _anchorIndex = next;
      }
    });
    _ensureVisible(next);
  }

  void _ensureVisible(int index) {
    if (!_scrollController.hasClients) return;
    final position = _scrollController.position;
    final double top;
    final double height;
    if (_viewMode == BrowserViewMode.list) {
      top = index * kEntryRowHeight;
      height = kEntryRowHeight;
    } else {
      top = 12 + (index ~/ _gridColumns) * (kGridTileHeight + 12);
      height = kGridTileHeight;
    }
    if (top < position.pixels) {
      _scrollController.jumpTo(top.clamp(0, position.maxScrollExtent));
    } else if (top + height > position.pixels + position.viewportDimension) {
      _scrollController.jumpTo(
        (top + height - position.viewportDimension + 8).clamp(
          0,
          position.maxScrollExtent,
        ),
      );
    }
  }

  @override
  void _selectAll() {
    setState(() => _selected = {for (final i in _visible) i.key});
  }

  void _clearSelection() {
    setState(() {
      _selected = {};
      _anchorIndex = null;
    });
  }

  void _openCursorOrSelection() {
    final selected = _selectedItems;
    if (selected.length == 1) {
      _openItem(selected.first);
    } else if (_cursorIndex != null && _cursorIndex! < _visible.length) {
      _openItem(_visible[_cursorIndex!]);
    }
  }

  @override
  void _openItem(BrowserItem item) {
    if (item.isDirectory) {
      _load(RemotePath.join(_currentPath, item.name));
    } else {
      _download([item]);
    }
  }

  // -------------------------------------------------------------------------
  // Construcción
  // -------------------------------------------------------------------------

  bool _dropEnabled(BuildContext context) {
    // desktop_drop entrega los eventos a TODOS los DropTarget montados, se
    // vean o no: se desactiva si la sección no es la visible (el shell la
    // mete en un TickerMode apagado) o si hay un diálogo encima.
    return TickerMode.valuesOf(context).enabled &&
        (ModalRoute.of(context)?.isCurrent ?? true);
  }

  @override
  Widget build(BuildContext context) {
    _visible = _computeVisible();
    final p = context.palette;

    return Scaffold(
      backgroundColor: p.canvas,
      body: CallbackShortcuts(
        bindings: {
          const SingleActivator(LogicalKeyboardKey.keyF, control: true): () =>
              _filterFocus.requestFocus(),
          const SingleActivator(LogicalKeyboardKey.keyU, control: true):
              _uploadPicked,
          const SingleActivator(LogicalKeyboardKey.keyN, control: true):
              _createFolder,
          const SingleActivator(
            LogicalKeyboardKey.keyN,
            control: true,
            shift: true,
          ): _createFolder,
          const SingleActivator(LogicalKeyboardKey.f5): _reload,
          const SingleActivator(LogicalKeyboardKey.keyR, control: true):
              _reload,
          const SingleActivator(LogicalKeyboardKey.arrowUp, alt: true): _goUp,
        },
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            _buildHeader(context),
            _buildActionBar(context),
            Expanded(
              child: Padding(
                padding: const EdgeInsets.fromLTRB(20, 0, 20, 16),
                child: DropTarget(
                  enable: _dropEnabled(context),
                  onDragEntered: (_) => setState(() => _dragging = true),
                  onDragExited: (_) => setState(() => _dragging = false),
                  onDragDone: (details) {
                    setState(() => _dragging = false);
                    _uploadLocalPaths([for (final f in details.files) f.path]);
                  },
                  child: Stack(
                    children: [
                      Positioned.fill(child: _buildContentCard(context)),
                      if (_dragging)
                        Positioned.fill(child: DropOverlay(path: _currentPath)),
                    ],
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildHeader(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(22, 20, 20, 8),
      child: Row(
        children: [
          Expanded(
            child: Align(
              alignment: Alignment.centerLeft,
              child: BreadcrumbBar(
                path: _currentPath,
                onNavigate: (path) => _load(path),
              ),
            ),
          ),
          const SizedBox(width: 16),
          SizedBox(width: 240, child: _buildFilterField(context)),
          const SizedBox(width: 8),
          ViewModeToggle(
            mode: _viewMode,
            onChanged: (mode) => setState(() => _viewMode = mode),
          ),
          const SizedBox(width: 4),
          IconButton(
            tooltip: 'Actualizar (F5)',
            icon: const Icon(Icons.refresh_rounded),
            onPressed: _reload,
          ),
        ],
      ),
    );
  }

  Widget _buildFilterField(BuildContext context) {
    final p = context.palette;
    return CallbackShortcuts(
      bindings: {
        const SingleActivator(LogicalKeyboardKey.escape): () {
          _filterController.clear();
          setState(() {});
          _listFocus.requestFocus();
        },
        const SingleActivator(LogicalKeyboardKey.arrowDown): () {
          _listFocus.requestFocus();
          _moveCursor(1);
        },
      },
      child: TextField(
        controller: _filterController,
        focusNode: _filterFocus,
        onChanged: (_) => setState(() {
          _cursorIndex = null;
        }),
        style: const TextStyle(fontSize: 13.5),
        decoration: InputDecoration(
          hintText: 'Filtrar en esta carpeta',
          fillColor: p.surface,
          contentPadding: const EdgeInsets.symmetric(
            vertical: 10,
            horizontal: 12,
          ),
          prefixIcon: const Icon(Icons.filter_list_rounded, size: 18),
          prefixIconConstraints: const BoxConstraints(minWidth: 38),
          suffixIcon: _filterController.text.isEmpty
              ? null
              : IconButton(
                  tooltip: 'Quitar filtro',
                  icon: const Icon(Icons.close_rounded, size: 16),
                  onPressed: () => setState(_filterController.clear),
                ),
        ),
      ),
    );
  }

  Widget _buildActionBar(BuildContext context) {
    final selected = _selectedItems;
    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 4, 20, 12),
      child: SizedBox(
        height: 40,
        child: Row(
          children: [
            Tooltip(
              message: 'Subir archivo',
              child: FilledButton.icon(
                onPressed: _uploadPicked,
                icon: const Icon(Icons.upload_rounded, size: 18),
                label: const Text('Subir'),
              ),
            ),
            const SizedBox(width: 8),
            OutlinedButton.icon(
              onPressed: _createFolder,
              icon: const Icon(Icons.create_new_folder_outlined, size: 18),
              label: const Text('Nueva carpeta'),
            ),
            const SizedBox(width: 16),
            Expanded(
              child: Align(
                alignment: Alignment.centerRight,
                child: AnimatedSwitcher(
                  duration: const Duration(milliseconds: 150),
                  child: selected.isEmpty
                      ? const SizedBox.shrink()
                      : SelectionBar(
                          key: const ValueKey('selection-bar'),
                          count: selected.length,
                          canDownload: selected.any((i) => !i.isDirectory),
                          onDownload: () => _download(selected),
                          onMove: () => _move(selected),
                          onTrash: () => _trash(selected),
                          onClear: _clearSelection,
                        ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildContentCard(BuildContext context) {
    final p = context.palette;
    return DecoratedBox(
      decoration: BoxDecoration(
        color: p.surface,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: p.border),
      ),
      child: ClipRRect(
        borderRadius: BorderRadius.circular(12),
        child: Column(
          children: [
            Expanded(child: _buildBody(context)),
            if (_state == _LoadState.loaded && !(_listing?.isEmpty ?? true))
              _buildStatusBar(context),
          ],
        ),
      ),
    );
  }

  Widget _buildBody(BuildContext context) {
    switch (_state) {
      case _LoadState.loading:
        return const LoadingState();
      case _LoadState.error:
        return ErrorState(
          message: _errorMessage ?? 'No se pudo completar la operación.',
          onRetry: _reload,
        );
      case _LoadState.loaded:
        final listing = _listing!;
        if (listing.isEmpty) {
          return GestureDetector(
            behavior: HitTestBehavior.opaque,
            onSecondaryTapUp: (d) => _showContextMenu(null, d.globalPosition),
            child: EmptyState(
              icon: Icons.cloud_upload_outlined,
              title: 'Esta carpeta está vacía',
              message:
                  'Arrastra archivos o carpetas aquí desde el Explorador, '
                  'o usa «Subir».',
              action: OutlinedButton.icon(
                onPressed: _createFolder,
                icon: const Icon(Icons.create_new_folder_outlined, size: 18),
                label: const Text('Crear una carpeta'),
              ),
            ),
          );
        }
        if (_visible.isEmpty) {
          return EmptyState(
            icon: Icons.search_off_rounded,
            title: 'Nada coincide con «${_filterController.text.trim()}»',
            message:
                'El filtro solo busca en esta carpeta. Para buscar en '
                'todas, usa el buscador de la barra lateral.',
            action: TextButton(
              onPressed: () => setState(_filterController.clear),
              child: const Text('Quitar filtro'),
            ),
          );
        }
        return _buildEntries(context);
    }
  }

  Widget _buildEntries(BuildContext context) {
    final callbacks = EntryCallbacks(
      onSelect: _handleSelect,
      onOpen: _openItem,
      onContextMenu: _showContextMenu,
      onDownload: (item) => _download([item]),
      onShare: _share,
      onTrash: (item) => _trash(
        _selected.contains(item.key) && _selected.length > 1
            ? _selectedItems
            : [item],
      ),
    );
    final isGrid = _viewMode == BrowserViewMode.grid;

    // Los atajos van POR ENCIMA del nodo de foco de la lista: los eventos
    // de teclado suben desde el nodo enfocado hacia sus ancestros, así que
    // un CallbackShortcuts por debajo de él nunca los vería.
    return CallbackShortcuts(
      bindings: {
        const SingleActivator(LogicalKeyboardKey.delete): () =>
            _trash(_selectedItems),
        const SingleActivator(LogicalKeyboardKey.f2): () {
          final s = _selectedItems;
          if (s.length == 1) _rename(s.first);
        },
        const SingleActivator(LogicalKeyboardKey.enter): _openCursorOrSelection,
        const SingleActivator(LogicalKeyboardKey.backspace): _goUp,
        const SingleActivator(LogicalKeyboardKey.keyA, control: true):
            _selectAll,
        const SingleActivator(LogicalKeyboardKey.escape): _clearSelection,
        const SingleActivator(LogicalKeyboardKey.arrowDown): () =>
            _moveCursor(isGrid ? _gridColumns : 1),
        const SingleActivator(LogicalKeyboardKey.arrowUp): () =>
            _moveCursor(isGrid ? -_gridColumns : -1),
        const SingleActivator(LogicalKeyboardKey.arrowDown, shift: true): () =>
            _moveCursor(isGrid ? _gridColumns : 1, extend: true),
        const SingleActivator(LogicalKeyboardKey.arrowUp, shift: true): () =>
            _moveCursor(isGrid ? -_gridColumns : -1, extend: true),
        if (isGrid)
          const SingleActivator(LogicalKeyboardKey.arrowRight): () =>
              _moveCursor(1),
        if (isGrid)
          const SingleActivator(LogicalKeyboardKey.arrowLeft): () =>
              _moveCursor(-1),
        const SingleActivator(LogicalKeyboardKey.home): () =>
            _moveCursor(-_visible.length),
        const SingleActivator(LogicalKeyboardKey.end): () =>
            _moveCursor(_visible.length),
        const SingleActivator(LogicalKeyboardKey.f10, shift: true):
            _openMenuAtCursor,
        const SingleActivator(LogicalKeyboardKey.contextMenu):
            _openMenuAtCursor,
      },
      child: Focus(
        focusNode: _listFocus,
        autofocus: true,
        // Repinta para mostrar u ocultar el contorno del cursor de teclado.
        onFocusChange: (_) => setState(() {}),
        child: GestureDetector(
          behavior: HitTestBehavior.translucent,
          onTap: () => _listFocus.requestFocus(),
          onSecondaryTapUp: (d) => _showContextMenu(null, d.globalPosition),
          child: isGrid
              ? FileGridView(
                  items: _visible,
                  selectedKeys: _selected,
                  callbacks: callbacks,
                  scrollController: _scrollController,
                  cursorIndex: _listFocus.hasFocus ? _cursorIndex : null,
                  onColumnsChanged: (columns) => _gridColumns = columns,
                )
              : FileListView(
                  items: _visible,
                  selectedKeys: _selected,
                  sort: _sort,
                  onSort: (field) =>
                      setState(() => _sort = _sort.toggle(field)),
                  callbacks: callbacks,
                  scrollController: _scrollController,
                  cursorIndex: _listFocus.hasFocus ? _cursorIndex : null,
                ),
        ),
      ),
    );
  }

  void _openMenuAtCursor() {
    final index =
        _cursorIndex ??
        (_selectedItems.isNotEmpty
            ? _visible.indexOf(_selectedItems.first)
            : null);
    final box = context.findRenderObject() as RenderBox?;
    if (box == null) return;
    final center = box.localToGlobal(box.size.center(Offset.zero));
    _showContextMenu(index, center);
  }

  Widget _buildStatusBar(BuildContext context) {
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    final listing = _listing!;
    final selected = _selectedItems;
    final String summary;
    if (selected.isNotEmpty) {
      final bytes = selected.fold<int>(0, (sum, i) => sum + (i.sizeBytes ?? 0));
      summary =
          '${pluralize(selected.length, 'elemento seleccionado', 'elementos seleccionados')}'
          '${bytes > 0 ? ' · ${formatBytes(bytes)}' : ''}';
    } else {
      final bytes = listing.files.fold<int>(0, (sum, f) => sum + f.sizeBytes);
      summary =
          '${pluralize(listing.directories.length, 'carpeta')} · '
          '${pluralize(listing.files.length, 'archivo')} · ${formatBytes(bytes)}';
    }
    return Container(
      height: 34,
      padding: const EdgeInsets.symmetric(horizontal: 16),
      decoration: BoxDecoration(
        color: p.surfaceMuted.withValues(alpha: 0.5),
        border: Border(top: BorderSide(color: p.border)),
      ),
      child: Row(
        children: [
          Expanded(
            child: Text(
              summary,
              style: text.bodySmall,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
          ),
          const SizedBox(width: 16),
          Flexible(
            child: Text(
              'Doble clic para abrir · Clic derecho para más opciones',
              style: text.bodySmall?.copyWith(fontSize: 11.5),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              textAlign: TextAlign.right,
            ),
          ),
        ],
      ),
    );
  }
}
