import 'package:flutter/material.dart';

import '../theme/app_palette.dart';
import '../widgets/file_type_icon.dart';
import 'transfer_queue.dart';

/// Panel inferior de transferencias: una barra resumen ("Subiendo 3
/// archivos · 42 %") que se despliega en la lista completa. Invisible
/// cuando no hay nada que mostrar.
class TransferPanel extends StatefulWidget {
  const TransferPanel({super.key, required this.queue});

  final TransferQueue queue;

  @override
  State<TransferPanel> createState() => _TransferPanelState();
}

class _TransferPanelState extends State<TransferPanel> {
  bool _expanded = false;

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: widget.queue,
      builder: (context, _) {
        final items = widget.queue.items;
        return AnimatedSize(
          duration: const Duration(milliseconds: 180),
          curve: Curves.easeOut,
          alignment: Alignment.bottomCenter,
          child: items.isEmpty
              ? const SizedBox(width: double.infinity)
              : _buildPanel(context, items),
        );
      },
    );
  }

  Widget _buildPanel(BuildContext context, List<TransferItem> items) {
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    final queue = widget.queue;
    final active = queue.activeCount;
    final failed = queue.failedCount;
    final uploads = items
        .where(
          (t) =>
              t.kind == TransferKind.upload &&
              t.status == TransferStatus.running,
        )
        .length;
    final downloads = active - uploads;

    final String summary;
    if (active > 0) {
      final parts = [
        if (uploads > 0) 'Subiendo $uploads',
        if (downloads > 0) 'Descargando $downloads',
      ];
      final pct = queue.overallProgress;
      summary =
          '${parts.join(' · ')}${pct != null ? ' · ${(pct * 100).round()} %' : ''}';
    } else if (failed > 0) {
      summary = failed == 1
          ? '1 transferencia con error'
          : '$failed transferencias con error';
    } else {
      summary = 'Transferencias completadas';
    }

    return Container(
      decoration: BoxDecoration(
        color: p.surface,
        border: Border(top: BorderSide(color: p.border)),
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (_expanded)
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 220),
              child: ListView.separated(
                shrinkWrap: true,
                padding: const EdgeInsets.fromLTRB(16, 8, 16, 4),
                itemCount: items.length,
                separatorBuilder: (_, _) => const SizedBox(height: 2),
                itemBuilder: (context, i) =>
                    _TransferRow(item: items[i], queue: queue),
              ),
            ),
          InkWell(
            onTap: () => setState(() => _expanded = !_expanded),
            child: SizedBox(
              height: 46,
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 16),
                child: Row(
                  children: [
                    _SummaryIcon(active: active > 0, failed: failed > 0),
                    const SizedBox(width: 12),
                    Expanded(
                      child: Column(
                        mainAxisAlignment: MainAxisAlignment.center,
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            summary,
                            style: text.bodyMedium?.copyWith(
                              fontWeight: FontWeight.w500,
                            ),
                          ),
                          if (active > 0) ...[
                            const SizedBox(height: 5),
                            SizedBox(
                              width: 260,
                              child: LinearProgressIndicator(
                                value: queue.overallProgress,
                                minHeight: 3,
                              ),
                            ),
                          ],
                        ],
                      ),
                    ),
                    if (active == 0)
                      TextButton(
                        onPressed: queue.clearFinished,
                        child: const Text('Limpiar'),
                      ),
                    Icon(
                      _expanded
                          ? Icons.expand_more_rounded
                          : Icons.expand_less_rounded,
                      color: p.textMuted,
                    ),
                  ],
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _SummaryIcon extends StatelessWidget {
  const _SummaryIcon({required this.active, required this.failed});

  final bool active;
  final bool failed;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    if (active) {
      return SizedBox(
        width: 20,
        height: 20,
        child: CircularProgressIndicator(strokeWidth: 2.2, color: p.accent),
      );
    }
    return Icon(
      failed ? Icons.error_outline_rounded : Icons.check_circle_rounded,
      color: failed ? p.danger : p.success,
      size: 20,
    );
  }
}

class _TransferRow extends StatelessWidget {
  const _TransferRow({required this.item, required this.queue});

  final TransferItem item;
  final TransferQueue queue;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    final isUpload = item.kind == TransferKind.upload;

    final Widget trailing = switch (item.status) {
      TransferStatus.running => Text(
        item.progress > 0 ? '${(item.progress * 100).round()} %' : '…',
        style: text.bodySmall,
      ),
      TransferStatus.done => Icon(
        Icons.check_rounded,
        size: 18,
        color: p.success,
      ),
      TransferStatus.failed => IconButton(
        tooltip: 'Descartar',
        icon: const Icon(Icons.close_rounded, size: 18),
        onPressed: () => queue.dismiss(item),
      ),
    };

    return SizedBox(
      height: 48,
      child: Row(
        children: [
          FileTypeIcon.forName(item.name, size: 18, boxed: true),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Icon(
                      isUpload
                          ? Icons.arrow_upward_rounded
                          : Icons.arrow_downward_rounded,
                      size: 13,
                      color: p.textMuted,
                    ),
                    const SizedBox(width: 4),
                    Flexible(
                      child: Text(
                        item.name,
                        overflow: TextOverflow.ellipsis,
                        style: text.bodyMedium?.copyWith(fontSize: 13.5),
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 4),
                if (item.status == TransferStatus.failed)
                  Text(
                    item.error ?? 'Error',
                    style: text.bodySmall?.copyWith(color: p.danger),
                    overflow: TextOverflow.ellipsis,
                  )
                else
                  LinearProgressIndicator(
                    value: item.status == TransferStatus.done
                        ? 1
                        : (item.progress == 0 ? null : item.progress),
                    minHeight: 3,
                    color: item.status == TransferStatus.done
                        ? p.success
                        : null,
                  ),
              ],
            ),
          ),
          const SizedBox(width: 12),
          SizedBox(width: 44, child: Center(child: trailing)),
        ],
      ),
    );
  }
}
