import 'package:flutter/material.dart';

import '../../../../core/format/formatters.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../../core/theme/app_theme.dart';
import '../../../../core/widgets/page_scaffold.dart';
import '../../domain/entities/pair_sync_outcome.dart';
import '../../domain/entities/pending_delete.dart';

/// Resultado de sincronizar UN par, extraído de `SyncSettingsPage` en el
/// slice 15 para poder repetirlo una vez por cada `PairSyncOutcome` de una
/// pasada de `MultiPairSyncCoordinator.syncAllNow`. Se pinta dentro de la
/// tarjeta de su carpeta: contadores como pastillas y, solo si hay algo
/// que revisar, las listas de borrados pendientes, conflictos y errores.
class PairSyncResultCard extends StatelessWidget {
  const PairSyncResultCard({
    super.key,
    required this.outcome,
    required this.disabled,
    required this.onConfirmPendingDeletes,
  });

  final PairSyncOutcome outcome;

  /// Deshabilita el botón "Revisar y confirmar borrados" mientras haya
  /// cualquier sincronización en curso -- mismo criterio que el resto de
  /// acciones de la página.
  final bool disabled;

  final void Function(List<PendingDelete> pending) onConfirmPendingDeletes;

  @override
  Widget build(BuildContext context) {
    final pal = context.palette;
    final text = Theme.of(context).textTheme;
    final startupError = outcome.startupError;

    if (startupError != null) {
      return Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(Icons.error_outline_rounded, size: 18, color: pal.danger),
          const SizedBox(width: 8),
          Expanded(
            child: Text(startupError, style: TextStyle(color: pal.danger)),
          ),
        ],
      );
    }

    final result = outcome.result!;
    final deleted = result.deletedRemote + result.deletedLocal;
    final clean =
        result.errors.isEmpty &&
        result.conflicts.isEmpty &&
        result.pendingDeletes.isEmpty;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Wrap(
          spacing: 6,
          runSpacing: 6,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            Icon(
              clean ? Icons.check_circle_rounded : Icons.info_rounded,
              size: 16,
              color: clean ? pal.success : pal.warning,
            ),
            Text(
              'Última: ${formatAgo(result.finishedAt)}',
              style: text.bodySmall?.copyWith(fontWeight: FontWeight.w500),
            ),
            const SizedBox(width: 4),
            StatusPill(
              label: '${result.downloaded} descargados',
              icon: Icons.arrow_downward_rounded,
            ),
            StatusPill(
              label: '${result.uploaded} subidos',
              icon: Icons.arrow_upward_rounded,
            ),
            StatusPill(
              label: '${result.skipped} ya al día',
              icon: Icons.check_rounded,
            ),
            if (deleted > 0)
              StatusPill(
                label: '$deleted borrados',
                icon: Icons.delete_outline_rounded,
              ),
            if (result.conflicts.isNotEmpty)
              StatusPill(
                label: '${result.conflicts.length} conflictos',
                icon: Icons.call_split_rounded,
                color: pal.warning,
                background: pal.warningSoft,
              ),
            if (result.errors.isNotEmpty)
              StatusPill(
                label: '${result.errors.length} errores',
                icon: Icons.error_outline_rounded,
                color: pal.danger,
                background: pal.dangerSoft,
              ),
          ],
        ),
        if (result.pendingDeletes.isNotEmpty)
          _Details(
            color: pal.warning,
            background: pal.warningSoft,
            title:
                '${result.pendingDeletes.length} borrados detectados no se ejecutaron todavía '
                '(lote grande, hace falta tu confirmación):',
            lines: [
              for (final pending in result.pendingDeletes) pending.displayPath,
            ],
            action: OutlinedButton.icon(
              onPressed: disabled
                  ? null
                  : () => onConfirmPendingDeletes(result.pendingDeletes),
              icon: const Icon(Icons.fact_check_outlined, size: 18),
              label: const Text('Revisar y confirmar borrados'),
            ),
          ),
        if (result.conflicts.isNotEmpty)
          _Details(
            color: pal.warning,
            background: pal.warningSoft,
            title:
                'Conflictos (se dejó una copia "(conflicto ...)" al lado, revísala y '
                'funde los cambios a mano):',
            lines: result.conflicts,
          ),
        if (result.errors.isNotEmpty)
          _Details(
            color: pal.danger,
            background: pal.dangerSoft,
            title: 'Errores:',
            lines: result.errors,
          ),
      ],
    );
  }
}

class _Details extends StatelessWidget {
  const _Details({
    required this.color,
    required this.background,
    required this.title,
    required this.lines,
    this.action,
  });

  final Color color;
  final Color background;
  final String title;
  final List<String> lines;
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    final text = Theme.of(context).textTheme;
    return Padding(
      padding: const EdgeInsets.only(top: 10),
      child: Container(
        width: double.infinity,
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: background,
          borderRadius: BorderRadius.circular(8),
          border: Border.all(color: color.withValues(alpha: 0.25)),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              title,
              style: text.bodySmall?.copyWith(
                color: color,
                fontWeight: FontWeight.w600,
              ),
            ),
            const SizedBox(height: 6),
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 150),
              child: ListView(
                shrinkWrap: true,
                children: [
                  for (final line in lines)
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 1),
                      child: Text(
                        '• $line',
                        style: text.bodySmall
                            ?.merge(monoTextStyle)
                            .copyWith(fontSize: 12.5),
                      ),
                    ),
                ],
              ),
            ),
            if (action != null) ...[const SizedBox(height: 10), action!],
          ],
        ),
      ),
    );
  }
}
