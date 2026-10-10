import 'package:flutter/material.dart';
import 'package:path/path.dart' as p;

import '../../../../core/di/service_locator.dart';
import '../../../../core/format/formatters.dart';
import '../../../../core/paths/remote_path.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../../core/widgets/confirm_dialog.dart';
import '../../../../core/widgets/page_scaffold.dart';
import '../../../../core/widgets/view_states.dart';
import '../../domain/entities/pair_sync_outcome.dart';
import '../../domain/entities/pending_delete.dart';
import '../../domain/entities/sync_direction.dart';
import '../../domain/entities/sync_pair.dart';
import '../../domain/entities/sync_pair_config.dart';
import '../../domain/repositories/sync_config_repository.dart';
import '../../domain/services/local_change_watcher_service.dart';
import '../../domain/services/multi_pair_sync_coordinator.dart';
import '../sync_activity.dart';
import '../widgets/pair_edit_dialog.dart';
import '../widgets/pair_sync_result_card.dart';

/// Carpetas vinculadas (pares carpeta-remota/carpeta-local, ADR-011 y
/// ADR-014): cada una en su tarjeta con su sentido, su último resultado y
/// sus acciones; más "Sincronizar todo". Los ajustes globales (auto-sync,
/// vigilancia, bandeja, arranque con Windows) viven ahora en Ajustes.
///
/// El estado de las sincronizaciones (en curso, mensajes, últimos
/// resultados) vive en [SyncActivity], compartido con el indicador de la
/// barra lateral y vivo aunque se salga de esta página.
class SyncSettingsPage extends StatefulWidget {
  const SyncSettingsPage({super.key});

  @override
  State<SyncSettingsPage> createState() => _SyncSettingsPageState();
}

class _SyncSettingsPageState extends State<SyncSettingsPage> {
  final SyncConfigRepository _configRepository = sl<SyncConfigRepository>();
  final MultiPairSyncCoordinator _coordinator = sl<MultiPairSyncCoordinator>();
  final LocalChangeWatcherService _watcherService =
      sl<LocalChangeWatcherService>();
  final SyncActivity _activity = sl<SyncActivity>();

  List<SyncPairConfig> _pairs = [];
  bool _loaded = false;

  @override
  void initState() {
    super.initState();
    _loadPairs();
  }

  Future<void> _loadPairs() async {
    final pairs = await _configRepository.readPairs();
    if (!mounted) return;
    setState(() {
      _pairs = pairs;
      _loaded = true;
    });
  }

  Future<void> _savePairs() async {
    await _configRepository.savePairs(_pairs);
    await _watcherService.updatePairs(_pairs);
    await _activity.refreshConfig();
  }

  Future<void> _addPair() async {
    final config = await showPairEditDialog(context, existingPairs: _pairs);
    if (config == null || !mounted) return;
    setState(() => _pairs = [..._pairs, config]);
    await _savePairs();
  }

  Future<void> _editPair(int index) async {
    final config = await showPairEditDialog(
      context,
      initial: _pairs[index],
      // Sin el propio par que se está editando -- si no, cualquier edición
      // sin cambiar las rutas "solaparía" consigo mismo.
      existingPairs: [..._pairs]..removeAt(index),
    );
    if (config == null || !mounted) return;
    setState(() {
      _pairs = [..._pairs]..[index] = config;
    });
    await _savePairs();
  }

  Future<void> _removePair(int index) async {
    final removed = _pairs[index];
    final confirmed = await showConfirmDialog(
      context,
      title: 'Desvincular carpeta',
      message:
          '${removed.pair.remotePath} → ${removed.pair.localPath}\n\n'
          'Se dejará de sincronizar. No se borra ningún archivo, ni en el '
          'servidor ni en este equipo.',
      confirmLabel: 'Desvincular',
    );
    if (!confirmed || !mounted) return;
    setState(() => _pairs = [..._pairs]..removeAt(index));
    _activity.forgetPair(removed.pair.stableKey);
    await _savePairs();
  }

  Future<void> _syncAll() => _runSync(_pairs);

  Future<void> _syncOnePair(SyncPairConfig config) => _runSync([config]);

  /// Compartido entre "Sincronizar todo", el botón de sincronizar un único
  /// par, y la re-sincronización que dispara `_confirmPendingDeletesFor`
  /// tras confirmar un lote de borrados de un par concreto (ADR-013/ADR-014)
  /// -- mismo flujo en los tres casos, solo cambia la lista de pares.
  Future<void> _runSync(
    List<SyncPairConfig> toSync, {
    Map<String, Set<String>>? confirmedDeletePathsByPair,
  }) async {
    if (toSync.isEmpty) return;
    _activity.beginManual();

    final outcomes = await _coordinator.syncAllNow(
      toSync,
      confirmedDeletePathsByPair: confirmedDeletePathsByPair,
      onStatus: (pair, status) => _activity.reportManualStatus(
        '${p.basename(pair.localPath)}: $status',
      ),
    );
    _activity
      ..recordOutcomes(outcomes)
      ..endManual();
    if (!mounted) return;

    // Solo un sync disparado desde aquí (nunca un tick automático) abre el
    // diálogo de confirmación, uno por par si hiciera falta -- ver el botón
    // "Revisar y confirmar borrados" de cada tarjeta para el caso de un lote
    // que llegó de un tick con la página ya abierta.
    for (final outcome in outcomes) {
      final pending = outcome.result?.pendingDeletes;
      if (pending != null && pending.isNotEmpty) {
        await _confirmPendingDeletesFor(outcome.pair, pending);
      }
    }
  }

  /// Muestra la lista real de borrados detectados para [pair] (nunca solo
  /// una cifra) y, si el usuario confirma, vuelve a sincronizar ESE par
  /// pasando sus claves -- ADR-013, guarda anti-"borrado masivo". Cancelar
  /// no hace nada más: el mismo lote reaparecerá como pendiente en la
  /// próxima sincronización de ese par.
  Future<void> _confirmPendingDeletesFor(
    SyncPair pair,
    List<PendingDelete> pending,
  ) async {
    if (!mounted || pending.isEmpty) return;

    final details = pending
        .map((d) {
          final hint = d.direction == DeleteDirection.toRemote
              ? 'se borraría en el servidor (papelera)'
              : 'se movería a la papelera local';
          return '${d.displayPath} -- $hint';
        })
        .join('\n');

    final confirmed = await showConfirmDialog(
      context,
      title: 'Confirmar ${pending.length} borrados',
      message:
          '${pair.remotePath} → ${pair.localPath}\n\n'
          'Estos archivos desaparecieron de un lado desde la última '
          'sincronización. Nada se borra para siempre -- van a una '
          'papelera, recuperable. Revísalos antes de confirmar:\n\n$details',
      confirmLabel: 'Borrar ${pending.length}',
      danger: true,
    );
    if (!confirmed || !mounted) return;

    // El par pudo editarse/quitarse entre el sync y esta confirmación; si
    // ya no está en la lista, se re-sincroniza igual con `Ambos` (el único
    // sentido que genera borrados pendientes) para no perder la
    // confirmación que el usuario acaba de dar.
    SyncPairConfig? config;
    for (final c in _pairs) {
      if (c.pair.stableKey == pair.stableKey) config = c;
    }
    config ??= SyncPairConfig(pair: pair, direction: SyncDirection.both);

    await _runSync(
      [config],
      confirmedDeletePathsByPair: {
        pair.stableKey: {for (final d in pending) d.key},
      },
    );
  }

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: _activity,
      builder: (context, _) {
        final busy = _activity.isBusy;
        return Scaffold(
          body: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              PageHeader(
                title: 'Sincronización',
                subtitle: 'Vincula carpetas del servidor con carpetas de este equipo.',
                actions: [
                  OutlinedButton.icon(
                    onPressed: busy ? null : _addPair,
                    icon: const Icon(Icons.add_rounded, size: 18),
                    label: const Text('Vincular carpeta'),
                  ),
                  FilledButton.icon(
                    onPressed: (busy || _pairs.isEmpty) ? null : _syncAll,
                    icon: _activity.manualRunning
                        ? const SizedBox(
                            width: 16,
                            height: 16,
                            child: CircularProgressIndicator(
                              strokeWidth: 2,
                              color: Colors.white,
                            ),
                          )
                        : const Icon(Icons.sync_rounded, size: 18),
                    label: const Text('Sincronizar todo ahora'),
                  ),
                ],
              ),
              if (busy) _BusyBanner(message: _activity.statusMessage),
              Expanded(child: _buildBody(context, busy)),
            ],
          ),
        );
      },
    );
  }

  Widget _buildBody(BuildContext context, bool busy) {
    if (!_loaded) return const LoadingState();
    if (_pairs.isEmpty) {
      return EmptyState(
        icon: Icons.sync_alt_rounded,
        title: 'Ninguna carpeta configurada todavía',
        message:
            'Vincula una carpeta del servidor con una de este equipo y '
            'NexusCloud mantendrá las dos al día.',
        action: FilledButton.icon(
          onPressed: _addPair,
          icon: const Icon(Icons.add_rounded, size: 18),
          label: const Text('Vincular una carpeta'),
        ),
      );
    }
    final outcomes = _activity.outcomesByKey;
    final lastAuto = _activity.lastAutoOutcome;
    return ListView(
      padding: const EdgeInsets.fromLTRB(20, 4, 20, 28),
      children: [
        for (var i = 0; i < _pairs.length; i++) ...[
          _PairCard(
            config: _pairs[i],
            outcome: outcomes[_pairs[i].pair.stableKey],
            disabled: busy,
            onSync: () => _syncOnePair(_pairs[i]),
            onEdit: () => _editPair(i),
            onRemove: () => _removePair(i),
            onConfirmPendingDeletes: (pending) =>
                _confirmPendingDeletesFor(_pairs[i].pair, pending),
          ),
          const SizedBox(height: 12),
        ],
        if (outcomes.isEmpty && lastAuto != null)
          Padding(
            padding: const EdgeInsets.only(top: 4, bottom: 12),
            // Sin resultados en esta sesión pero sí un intento automático
            // de una anterior -- solo un resumen de texto (ver por qué en
            // SyncConfigRepository), no el desglose completo.
            child: InlineAlert(
              message:
                  'Última sincronización automática '
                  '(${formatDateTime(lastAuto.at)}): ${lastAuto.summary}',
            ),
          ),
        const SizedBox(height: 4),
        const _HowItWorks(),
      ],
    );
  }
}

class _BusyBanner extends StatelessWidget {
  const _BusyBanner({required this.message});

  final String? message;

  @override
  Widget build(BuildContext context) {
    final pal = context.palette;
    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 0, 20, 12),
      child: Container(
        padding: const EdgeInsets.fromLTRB(14, 12, 14, 12),
        decoration: BoxDecoration(
          color: pal.accentSoft,
          borderRadius: BorderRadius.circular(10),
          border: Border.all(color: pal.accent.withValues(alpha: 0.25)),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                SizedBox(
                  width: 16,
                  height: 16,
                  child: CircularProgressIndicator(
                    strokeWidth: 2,
                    color: pal.accent,
                  ),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: Text(
                    message ?? 'Sincronizando…',
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      color: pal.accentOnSoft,
                      fontWeight: FontWeight.w500,
                    ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 10),
            const LinearProgressIndicator(minHeight: 3),
          ],
        ),
      ),
    );
  }
}

class _PairCard extends StatelessWidget {
  const _PairCard({
    required this.config,
    required this.outcome,
    required this.disabled,
    required this.onSync,
    required this.onEdit,
    required this.onRemove,
    required this.onConfirmPendingDeletes,
  });

  final SyncPairConfig config;
  final PairSyncOutcome? outcome;
  final bool disabled;
  final VoidCallback onSync;
  final VoidCallback onEdit;
  final VoidCallback onRemove;
  final void Function(List<PendingDelete> pending) onConfirmPendingDeletes;

  @override
  Widget build(BuildContext context) {
    final pal = context.palette;
    final text = Theme.of(context).textTheme;
    final pair = config.pair;
    final remoteLabel = [
      'Mis archivos',
      ...RemotePath.segments(pair.remotePath),
    ].join(' › ');
    final (IconData dirIcon, String dirHint) = switch (config.direction) {
      SyncDirection.download => (
        Icons.arrow_downward_rounded,
        'Solo trae del servidor',
      ),
      SyncDirection.upload => (
        Icons.arrow_upward_rounded,
        'Solo envía al servidor',
      ),
      SyncDirection.both => (
        Icons.swap_vert_rounded,
        'Mantiene los dos lados iguales',
      ),
    };

    return Card(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(18, 16, 12, 16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      _Endpoint(
                        icon: Icons.cloud_outlined,
                        label: remoteLabel,
                        caption: 'Servidor',
                      ),
                      Padding(
                        padding: const EdgeInsets.only(left: 9),
                        child: Row(
                          children: [
                            Container(
                              width: 1.5,
                              height: 14,
                              color: pal.border,
                            ),
                            const SizedBox(width: 17),
                            Icon(dirIcon, size: 14, color: pal.accent),
                            const SizedBox(width: 4),
                            Text(
                              '${config.direction.label} · $dirHint',
                              style: text.bodySmall?.copyWith(
                                color: pal.accentOnSoft,
                              ),
                            ),
                          ],
                        ),
                      ),
                      _Endpoint(
                        icon: Icons.computer_rounded,
                        label: pair.localPath,
                        caption: 'Este equipo',
                      ),
                    ],
                  ),
                ),
                const SizedBox(width: 12),
                if (config.autoSyncEnabled)
                  const Padding(
                    padding: EdgeInsets.only(right: 8),
                    child: StatusPill(
                      label: 'Automática',
                      icon: Icons.schedule_rounded,
                    ),
                  ),
                IconButton(
                  icon: const Icon(Icons.sync_rounded),
                  tooltip: 'Sincronizar solo esta carpeta',
                  onPressed: disabled ? null : onSync,
                ),
                IconButton(
                  icon: const Icon(Icons.edit_outlined),
                  tooltip: 'Editar',
                  onPressed: disabled ? null : onEdit,
                ),
                IconButton(
                  icon: const Icon(Icons.link_off_rounded),
                  tooltip: 'Desvincular',
                  onPressed: disabled ? null : onRemove,
                ),
              ],
            ),
            if (outcome != null) ...[
              const SizedBox(height: 12),
              const Divider(height: 1),
              const SizedBox(height: 12),
              PairSyncResultCard(
                outcome: outcome!,
                disabled: disabled,
                onConfirmPendingDeletes: onConfirmPendingDeletes,
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _Endpoint extends StatelessWidget {
  const _Endpoint({
    required this.icon,
    required this.label,
    required this.caption,
  });

  final IconData icon;
  final String label;
  final String caption;

  @override
  Widget build(BuildContext context) {
    final pal = context.palette;
    final text = Theme.of(context).textTheme;
    return Row(
      children: [
        Container(
          width: 20,
          height: 20,
          alignment: Alignment.center,
          child: Icon(icon, size: 18, color: pal.textSecondary),
        ),
        const SizedBox(width: 10),
        Flexible(
          child: Text(
            label,
            overflow: TextOverflow.ellipsis,
            style: text.bodyMedium?.copyWith(fontWeight: FontWeight.w500),
          ),
        ),
        const SizedBox(width: 8),
        Text(caption, style: text.bodySmall?.copyWith(fontSize: 11.5)),
      ],
    );
  }
}

class _HowItWorks extends StatelessWidget {
  const _HowItWorks();

  @override
  Widget build(BuildContext context) {
    final pal = context.palette;
    final text = Theme.of(context).textTheme;
    Widget item(IconData icon, String title, String body) => Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, size: 18, color: pal.accent),
          const SizedBox(width: 12),
          Expanded(
            child: Text.rich(
              TextSpan(
                children: [
                  TextSpan(
                    text: '$title. ',
                    style: const TextStyle(fontWeight: FontWeight.w600),
                  ),
                  TextSpan(text: body),
                ],
              ),
              style: text.bodyMedium?.copyWith(
                color: pal.textSecondary,
                fontSize: 13.5,
              ),
            ),
          ),
        ],
      ),
    );
    return Card(
      clipBehavior: Clip.antiAlias,
      child: Theme(
        data: Theme.of(context).copyWith(dividerColor: Colors.transparent),
        child: ExpansionTile(
          leading: Icon(Icons.help_outline_rounded, color: pal.textMuted),
          title: Text(
            '¿Cómo funciona la sincronización?',
            style: text.titleSmall,
          ),
          childrenPadding: const EdgeInsets.fromLTRB(20, 0, 20, 8),
          children: [
            item(
              Icons.arrow_downward_rounded,
              'Descargar y Subir',
              'Solo copian en un sentido y nunca borran: un archivo que falte en '
                  'el lado no tocado se vuelve a traer del otro.',
            ),
            item(
              Icons.swap_vert_rounded,
              'Ambos',
              'Reconcilia los dos lados. Si un archivo cambió en los dos sitios no se '
                  'sobrescribe nada: se deja una copia «(conflicto …)» al lado.',
            ),
            item(
              Icons.delete_sweep_outlined,
              'Borrados',
              'En «Ambos», borrar en un lado borra en el otro (a una papelera, '
                  'recuperable). Si son muchos de golpe se te pide confirmar viendo la lista real.',
            ),
            item(
              Icons.schedule_rounded,
              'Automática',
              'Si la activas en Ajustes, cubre todas las carpetas mientras la app esté '
                  'abierta; con «Minimizar a la bandeja» sigue funcionando con la ventana cerrada.',
            ),
          ],
        ),
      ),
    );
  }
}
