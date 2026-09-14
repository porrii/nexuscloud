import 'package:flutter/material.dart';

import '../../domain/entities/pair_sync_outcome.dart';
import '../../domain/entities/pending_delete.dart';

/// Resultado de sincronizar UN par, extraído de `SyncSettingsPage` en el
/// slice 15 para poder repetirlo una vez por cada `PairSyncOutcome` de una
/// pasada de `MultiPairSyncCoordinator.syncAllNow` -- antes vivía inline
/// porque solo existía un par.
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
    final startupError = outcome.startupError;
    final title = Text(
      '${outcome.pair.remotePath} → ${outcome.pair.localPath}',
      style: Theme.of(context).textTheme.titleSmall,
      overflow: TextOverflow.ellipsis,
    );

    if (startupError != null) {
      return Padding(
        padding: const EdgeInsets.only(bottom: 12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            title,
            const SizedBox(height: 4),
            Text(
              startupError,
              style: TextStyle(color: Theme.of(context).colorScheme.error),
            ),
          ],
        ),
      );
    }

    final result = outcome.result!;
    return Padding(
      padding: const EdgeInsets.only(bottom: 16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          title,
          const SizedBox(height: 4),
          Text(
            '${result.downloaded} descargados, ${result.uploaded} subidos, '
            '${result.skipped} ya al día, '
            '${result.deletedRemote + result.deletedLocal} borrados, '
            '${result.conflicts.length} conflictos, '
            '${result.errors.length} errores',
          ),
          if (result.pendingDeletes.isNotEmpty) ...[
            const SizedBox(height: 8),
            Text(
              '${result.pendingDeletes.length} borrados detectados no se '
              'ejecutaron todavía (lote grande, hace falta tu confirmación):',
              style: Theme.of(context).textTheme.bodySmall,
            ),
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 160),
              child: ListView(
                shrinkWrap: true,
                children: [
                  for (final pending in result.pendingDeletes)
                    Text('• ${pending.displayPath}'),
                ],
              ),
            ),
            const SizedBox(height: 8),
            OutlinedButton(
              onPressed: disabled
                  ? null
                  : () => onConfirmPendingDeletes(result.pendingDeletes),
              child: const Text('Revisar y confirmar borrados'),
            ),
          ],
          if (result.conflicts.isNotEmpty) ...[
            const SizedBox(height: 8),
            Text(
              'Conflictos (se dejó una copia "(conflicto ...)" al lado, '
              'revísala y funde los cambios a mano):',
              style: Theme.of(context).textTheme.bodySmall,
            ),
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 160),
              child: ListView(
                shrinkWrap: true,
                children: [
                  for (final name in result.conflicts) Text('• $name'),
                ],
              ),
            ),
          ],
          if (result.errors.isNotEmpty) ...[
            const SizedBox(height: 8),
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 200),
              child: ListView(
                shrinkWrap: true,
                children: [
                  for (final error in result.errors)
                    Text(
                      '• $error',
                      style: TextStyle(color: Theme.of(context).colorScheme.error),
                    ),
                ],
              ),
            ),
          ],
        ],
      ),
    );
  }
}
