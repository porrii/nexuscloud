import 'package:flutter/material.dart';

import '../../../../core/format/formatters.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../../core/widgets/file_type_icon.dart';
import '../browser_item.dart';

/// Lo que una fila o tarjeta puede pedir al explorador. La lógica (qué
/// significa seleccionar con Ctrl, cómo se borra...) vive en la página;
/// las vistas solo informan de gestos.
class EntryCallbacks {
  const EntryCallbacks({
    required this.onSelect,
    required this.onOpen,
    required this.onContextMenu,
    required this.onDownload,
    required this.onShare,
    required this.onTrash,
  });

  final void Function(int index) onSelect;
  final void Function(BrowserItem item) onOpen;
  final void Function(int index, Offset globalPosition) onContextMenu;
  final void Function(BrowserItem item) onDownload;
  final void Function(BrowserItem item) onShare;
  final void Function(BrowserItem item) onTrash;
}

const double kEntryRowHeight = 46;
const _doubleClickWindow = Duration(milliseconds: 380);

/// Detecta el doble clic a mano en vez de con `onDoubleTap`: este último
/// retrasa cada clic simple hasta descartar el doble, y la selección se
/// notaría lenta.
mixin _ClickTracker<T extends StatefulWidget> on State<T> {
  DateTime? _lastTap;

  bool registerTapIsDouble() {
    final now = DateTime.now();
    final isDouble =
        _lastTap != null && now.difference(_lastTap!) < _doubleClickWindow;
    _lastTap = isDouble ? null : now;
    return isDouble;
  }
}

// ---------------------------------------------------------------------------
// Vista de lista
// ---------------------------------------------------------------------------

class FileListView extends StatelessWidget {
  const FileListView({
    super.key,
    required this.items,
    required this.selectedKeys,
    required this.sort,
    required this.onSort,
    required this.callbacks,
    required this.scrollController,
    this.cursorIndex,
  });

  final List<BrowserItem> items;
  final Set<String> selectedKeys;
  final SortSpec sort;
  final ValueChanged<SortField> onSort;
  final EntryCallbacks callbacks;
  final ScrollController scrollController;
  final int? cursorIndex;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final showModified = constraints.maxWidth >= 640;
        final showSize = constraints.maxWidth >= 480;
        return Column(
          children: [
            _ListHeader(
              sort: sort,
              onSort: onSort,
              showModified: showModified,
              showSize: showSize,
            ),
            Expanded(
              child: Scrollbar(
                controller: scrollController,
                child: ListView.builder(
                  controller: scrollController,
                  itemExtent: kEntryRowHeight,
                  padding: const EdgeInsets.only(bottom: 12),
                  itemCount: items.length,
                  itemBuilder: (context, index) {
                    final item = items[index];
                    return _EntryRow(
                      key: ValueKey('entry-${item.key}'),
                      item: item,
                      index: index,
                      selected: selectedKeys.contains(item.key),
                      hasCursor: cursorIndex == index,
                      showModified: showModified,
                      showSize: showSize,
                      callbacks: callbacks,
                    );
                  },
                ),
              ),
            ),
          ],
        );
      },
    );
  }
}

class _ListHeader extends StatelessWidget {
  const _ListHeader({
    required this.sort,
    required this.onSort,
    required this.showModified,
    required this.showSize,
  });

  final SortSpec sort;
  final ValueChanged<SortField> onSort;
  final bool showModified;
  final bool showSize;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return Container(
      height: 38,
      padding: const EdgeInsets.symmetric(horizontal: 12),
      decoration: BoxDecoration(
        border: Border(bottom: BorderSide(color: p.border)),
      ),
      child: Row(
        children: [
          const SizedBox(width: 40),
          Expanded(
            child: _SortButton(
              label: 'Nombre',
              field: SortField.name,
              sort: sort,
              onSort: onSort,
            ),
          ),
          if (showSize)
            SizedBox(
              width: 100,
              child: _SortButton(
                label: 'Tamaño',
                field: SortField.size,
                sort: sort,
                onSort: onSort,
                alignEnd: true,
              ),
            ),
          if (showModified)
            SizedBox(
              width: 150,
              child: Padding(
                padding: const EdgeInsets.only(left: 24),
                child: _SortButton(
                  label: 'Modificado',
                  field: SortField.modified,
                  sort: sort,
                  onSort: onSort,
                ),
              ),
            ),
          const SizedBox(width: _kActionsWidth),
        ],
      ),
    );
  }
}

const double _kActionsWidth = 160;

class _SortButton extends StatelessWidget {
  const _SortButton({
    required this.label,
    required this.field,
    required this.sort,
    required this.onSort,
    this.alignEnd = false,
  });

  final String label;
  final SortField field;
  final SortSpec sort;
  final ValueChanged<SortField> onSort;
  final bool alignEnd;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final active = sort.field == field;
    final style = Theme.of(context).textTheme.labelSmall
        ?.copyWith(color: active ? p.textPrimary : p.textMuted);
    return Align(
      alignment: alignEnd ? Alignment.centerRight : Alignment.centerLeft,
      child: Semantics(
        button: true,
        label: 'Ordenar por $label',
        child: InkWell(
          borderRadius: BorderRadius.circular(6),
          onTap: () => onSort(field),
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 4),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Flexible(
                  child: Text(
                    label.toUpperCase(),
                    style: style,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                if (active) ...[
                  const SizedBox(width: 2),
                  Icon(
                    sort.ascending
                        ? Icons.arrow_upward_rounded
                        : Icons.arrow_downward_rounded,
                    size: 13,
                    color: p.textPrimary,
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _EntryRow extends StatefulWidget {
  const _EntryRow({
    super.key,
    required this.item,
    required this.index,
    required this.selected,
    required this.hasCursor,
    required this.showModified,
    required this.showSize,
    required this.callbacks,
  });

  final BrowserItem item;
  final int index;
  final bool selected;
  final bool hasCursor;
  final bool showModified;
  final bool showSize;
  final EntryCallbacks callbacks;

  @override
  State<_EntryRow> createState() => _EntryRowState();
}

class _EntryRowState extends State<_EntryRow> with _ClickTracker {
  bool _hovered = false;

  void _handleTap() {
    if (registerTapIsDouble()) {
      widget.callbacks.onOpen(widget.item);
    } else {
      widget.callbacks.onSelect(widget.index);
    }
  }

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    final item = widget.item;
    final selected = widget.selected;

    final background = selected
        ? p.accentSoft
        : _hovered
        ? p.surfaceMuted.withValues(alpha: 0.7)
        : Colors.transparent;

    return MouseRegion(
      onEnter: (_) => setState(() => _hovered = true),
      onExit: (_) => setState(() => _hovered = false),
      child: GestureDetector(
        behavior: HitTestBehavior.opaque,
        onSecondaryTapUp: (d) =>
            widget.callbacks.onContextMenu(widget.index, d.globalPosition),
        onTap: _handleTap,
        child: Container(
          margin: const EdgeInsets.symmetric(horizontal: 6, vertical: 1),
          padding: const EdgeInsets.symmetric(horizontal: 6),
          decoration: BoxDecoration(
            color: background,
            borderRadius: BorderRadius.circular(8),
            border: widget.hasCursor && !selected
                ? Border.all(color: p.accent.withValues(alpha: 0.5))
                : null,
          ),
          child: Row(
            children: [
              SizedBox(
                width: 40,
                child: Center(
                  child: FileTypeIcon.forName(
                    item.name,
                    mimeType: item.mimeType,
                    isDirectory: item.isDirectory,
                    size: 22,
                  ),
                ),
              ),
              Expanded(
                child: _EntryName(
                  item: item,
                  selected: selected,
                  onOpen: () => widget.callbacks.onOpen(item),
                ),
              ),
              if (widget.showSize)
                SizedBox(
                  width: 100,
                  child: Text(
                    item.sizeBytes == null ? '—' : formatBytes(item.sizeBytes!),
                    textAlign: TextAlign.right,
                    style: text.bodySmall?.copyWith(color: p.textSecondary),
                  ),
                ),
              if (widget.showModified)
                SizedBox(
                  width: 150,
                  child: Padding(
                    padding: const EdgeInsets.only(left: 30),
                    child: Text(
                      formatRelativeDate(item.modifiedAt),
                      overflow: TextOverflow.ellipsis,
                      style: text.bodySmall?.copyWith(color: p.textSecondary),
                    ),
                  ),
                ),
              SizedBox(
                width: _kActionsWidth,
                child: _RowActions(
                  item: item,
                  index: widget.index,
                  visible: _hovered || selected,
                  callbacks: widget.callbacks,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _EntryName extends StatefulWidget {
  const _EntryName({
    required this.item,
    required this.selected,
    required this.onOpen,
  });

  final BrowserItem item;
  final bool selected;
  final VoidCallback onOpen;

  @override
  State<_EntryName> createState() => _EntryNameState();
}

/// Nombre de la fila. En las carpetas es un enlace (como en la web): un
/// clic en el nombre entra; un clic en el resto de la fila selecciona.
class _EntryNameState extends State<_EntryName> {
  bool _hovered = false;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final style = Theme.of(context).textTheme.bodyMedium?.copyWith(
      fontWeight: FontWeight.w500,
      color: widget.selected ? p.accentOnSoft : p.textPrimary,
      decoration: _hovered ? TextDecoration.underline : null,
      decorationColor: p.accent,
    );
    final label = Text(
      widget.item.name,
      overflow: TextOverflow.ellipsis,
      maxLines: 1,
      style: style,
    );
    if (!widget.item.isDirectory) {
      return Align(alignment: Alignment.centerLeft, child: label);
    }
    return Align(
      alignment: Alignment.centerLeft,
      child: MouseRegion(
        cursor: SystemMouseCursors.click,
        onEnter: (_) => setState(() => _hovered = true),
        onExit: (_) => setState(() => _hovered = false),
        child: GestureDetector(onTap: widget.onOpen, child: label),
      ),
    );
  }
}

/// Acciones rápidas de una fila: visibles al pasar el ratón o con la fila
/// seleccionada. Siguen en el árbol aunque no se vean (con opacidad 0),
/// así que también están al alcance del teclado y de los lectores de
/// pantalla; el resto de acciones vive en "Más acciones" / clic derecho.
class _RowActions extends StatelessWidget {
  const _RowActions({
    required this.item,
    required this.index,
    required this.visible,
    required this.callbacks,
  });

  final BrowserItem item;
  final int index;
  final bool visible;
  final EntryCallbacks callbacks;

  @override
  Widget build(BuildContext context) {
    return AnimatedOpacity(
      opacity: visible ? 1 : 0,
      duration: const Duration(milliseconds: 120),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.end,
        children: [
          if (!item.isDirectory)
            _SmallAction(
              tooltip: 'Descargar',
              icon: Icons.download_rounded,
              onPressed: () => callbacks.onDownload(item),
            ),
          _SmallAction(
            tooltip: 'Compartir',
            icon: Icons.person_add_alt_outlined,
            onPressed: () => callbacks.onShare(item),
          ),
          _SmallAction(
            tooltip: 'Eliminar',
            icon: Icons.delete_outline_rounded,
            onPressed: () => callbacks.onTrash(item),
          ),
          Builder(
            builder: (context) => _SmallAction(
              tooltip: 'Más acciones',
              icon: Icons.more_horiz_rounded,
              onPressed: () {
                final box = context.findRenderObject() as RenderBox;
                final position = box.localToGlobal(Offset(0, box.size.height));
                callbacks.onContextMenu(index, position);
              },
            ),
          ),
        ],
      ),
    );
  }
}

class _SmallAction extends StatelessWidget {
  const _SmallAction({
    required this.tooltip,
    required this.icon,
    required this.onPressed,
  });

  final String tooltip;
  final IconData icon;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    return IconButton(
      tooltip: tooltip,
      icon: Icon(icon, size: 18),
      onPressed: onPressed,
      style: IconButton.styleFrom(
        minimumSize: const Size(32, 32),
        padding: const EdgeInsets.all(6),
        // Área de pulsación ajustada al icono: es una app de escritorio
        // con ratón, y cuatro áreas de 48 px no caben en la fila.
        tapTargetSize: MaterialTapTargetSize.shrinkWrap,
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// Vista de cuadrícula
// ---------------------------------------------------------------------------

const double kGridTileWidth = 156;
const double kGridTileHeight = 148;

class FileGridView extends StatelessWidget {
  const FileGridView({
    super.key,
    required this.items,
    required this.selectedKeys,
    required this.callbacks,
    required this.scrollController,
    required this.onColumnsChanged,
    this.cursorIndex,
  });

  final List<BrowserItem> items;
  final Set<String> selectedKeys;
  final EntryCallbacks callbacks;
  final ScrollController scrollController;
  final ValueChanged<int> onColumnsChanged;
  final int? cursorIndex;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final columns = ((constraints.maxWidth - 24) / (kGridTileWidth + 12))
            .floor()
            .clamp(1, 20);
        WidgetsBinding.instance.addPostFrameCallback(
          (_) => onColumnsChanged(columns),
        );
        return Scrollbar(
          controller: scrollController,
          child: GridView.builder(
            controller: scrollController,
            padding: const EdgeInsets.all(12),
            gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
              crossAxisCount: columns,
              mainAxisExtent: kGridTileHeight,
              crossAxisSpacing: 12,
              mainAxisSpacing: 12,
            ),
            itemCount: items.length,
            itemBuilder: (context, index) {
              final item = items[index];
              return _GridTile(
                key: ValueKey('entry-${item.key}'),
                item: item,
                index: index,
                selected: selectedKeys.contains(item.key),
                hasCursor: cursorIndex == index,
                callbacks: callbacks,
              );
            },
          ),
        );
      },
    );
  }
}

class _GridTile extends StatefulWidget {
  const _GridTile({
    super.key,
    required this.item,
    required this.index,
    required this.selected,
    required this.hasCursor,
    required this.callbacks,
  });

  final BrowserItem item;
  final int index;
  final bool selected;
  final bool hasCursor;
  final EntryCallbacks callbacks;

  @override
  State<_GridTile> createState() => _GridTileState();
}

class _GridTileState extends State<_GridTile> with _ClickTracker {
  bool _hovered = false;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    final item = widget.item;
    final selected = widget.selected;
    final meta = item.isDirectory
        ? 'Carpeta'
        : '${formatBytes(item.sizeBytes ?? 0)} · ${formatRelativeDate(item.modifiedAt)}';

    return MouseRegion(
      onEnter: (_) => setState(() => _hovered = true),
      onExit: (_) => setState(() => _hovered = false),
      child: GestureDetector(
        behavior: HitTestBehavior.opaque,
        onSecondaryTapUp: (d) =>
            widget.callbacks.onContextMenu(widget.index, d.globalPosition),
        onTap: () {
          if (registerTapIsDouble()) {
            widget.callbacks.onOpen(item);
          } else {
            widget.callbacks.onSelect(widget.index);
          }
        },
        child: AnimatedContainer(
          duration: const Duration(milliseconds: 120),
          decoration: BoxDecoration(
            color: selected
                ? p.accentSoft
                : (_hovered
                      ? p.surfaceMuted.withValues(alpha: 0.6)
                      : p.surface),
            borderRadius: BorderRadius.circular(12),
            border: Border.all(
              color: selected || widget.hasCursor
                  ? p.accent.withValues(alpha: 0.6)
                  : p.border,
            ),
          ),
          child: Stack(
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(12, 18, 12, 10),
                child: Column(
                  children: [
                    FileTypeIcon.forName(
                      item.name,
                      mimeType: item.mimeType,
                      isDirectory: item.isDirectory,
                      size: 30,
                      boxed: true,
                    ),
                    const SizedBox(height: 12),
                    Text(
                      item.name,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      textAlign: TextAlign.center,
                      style: text.bodyMedium?.copyWith(
                        fontWeight: FontWeight.w500,
                        fontSize: 13,
                        height: 1.25,
                        color: selected ? p.accentOnSoft : p.textPrimary,
                      ),
                    ),
                    const Spacer(),
                    Text(
                      meta,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: text.bodySmall?.copyWith(fontSize: 11.5),
                    ),
                  ],
                ),
              ),
              Positioned(
                top: 4,
                right: 4,
                child: AnimatedOpacity(
                  opacity: _hovered || selected ? 1 : 0,
                  duration: const Duration(milliseconds: 120),
                  child: Builder(
                    builder: (context) => _SmallAction(
                      tooltip: 'Más acciones',
                      icon: Icons.more_horiz_rounded,
                      onPressed: () {
                        final box = context.findRenderObject() as RenderBox;
                        widget.callbacks.onContextMenu(
                          widget.index,
                          box.localToGlobal(Offset(0, box.size.height)),
                        );
                      },
                    ),
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
