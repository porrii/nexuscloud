import 'package:flutter/material.dart';

import '../../../../core/format/formatters.dart';
import '../../../../core/network/api_exception.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../../core/widgets/page_scaffold.dart';
import '../../../../core/widgets/view_states.dart';
import '../../data/admin_models.dart';
import '../../data/admin_service.dart';
import '../widgets/admin_feedback.dart';
import '../widgets/quota_editor.dart';

/// Grupos (§22) y su cuota por miembro (§24). Para meter a alguien en un
/// grupo: pestaña Usuarios → «Añadir a un grupo». La API todavía no
/// permite ver los miembros de un grupo ni sacar a nadie de él.
class GroupsTab extends StatefulWidget {
  const GroupsTab({super.key, required this.service});

  final AdminService service;

  @override
  State<GroupsTab> createState() => _GroupsTabState();
}

class _GroupsTabState extends State<GroupsTab> {
  List<AdminGroup>? _groups;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final groups = await widget.service.listGroups();
      groups.sort(
        (a, b) => a.name.toLowerCase().compareTo(b.name.toLowerCase()),
      );
      if (mounted) setState(() => _groups = groups);
    } on ApiException catch (e) {
      if (mounted) setState(() => _error = e.message);
    }
  }

  Future<void> _edit({AdminGroup? group}) async {
    final saved = await showDialog<AdminGroup>(
      context: context,
      builder: (_) => _GroupDialog(service: widget.service, group: group),
    );
    if (saved == null || !mounted) return;
    showAdminToast(
      context,
      group == null ? 'Grupo «${saved.name}» creado.' : 'Cuota actualizada.',
    );
    _load();
  }

  @override
  Widget build(BuildContext context) {
    final groups = _groups;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SubToolbar(
          leading: Text(
            groups == null
                ? 'Grupos'
                : '${pluralize(groups.length, 'grupo')} · la cuota de grupo se '
                      'aplica a cada miembro que no tenga una propia',
            style: Theme.of(context).textTheme.bodySmall,
            overflow: TextOverflow.ellipsis,
          ),
          actions: [
            IconButton(
              tooltip: 'Actualizar',
              onPressed: _load,
              icon: const Icon(Icons.refresh_rounded),
            ),
            const SizedBox(width: 4),
            FilledButton.icon(
              onPressed: () => _edit(),
              icon: const Icon(Icons.group_add_outlined, size: 18),
              label: const Text('Nuevo grupo'),
            ),
          ],
        ),
        Expanded(child: _buildBody(context)),
      ],
    );
  }

  Widget _buildBody(BuildContext context) {
    if (_error != null) return ErrorState(message: _error!, onRetry: _load);
    final groups = _groups;
    if (groups == null) return const LoadingState();
    if (groups.isEmpty) {
      return EmptyState(
        icon: Icons.groups_outlined,
        title: 'Todavía no hay grupos',
        message:
            'Sirven para compartir con varias personas a la vez y para dar '
            'la misma cuota a todos sus miembros.',
        action: OutlinedButton.icon(
          onPressed: () => _edit(),
          icon: const Icon(Icons.group_add_outlined, size: 18),
          label: const Text('Crear un grupo'),
        ),
      );
    }
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    return ListView.separated(
      padding: const EdgeInsets.symmetric(vertical: 6),
      itemCount: groups.length,
      separatorBuilder: (_, _) => const Divider(height: 1),
      itemBuilder: (context, index) {
        final group = groups[index];
        return ListTile(
          key: ValueKey('group-${group.id}'),
          contentPadding: const EdgeInsets.symmetric(horizontal: 20),
          leading: Icon(Icons.groups_rounded, color: p.accent),
          title: Text(
            group.name,
            style: text.bodyMedium?.copyWith(fontWeight: FontWeight.w600),
          ),
          subtitle: Text(
            'Cuota por miembro: '
            '${describeQuota(group.quotaBytes, inheritLabel: 'sin cuota de grupo')}',
          ),
          trailing: TextButton.icon(
            onPressed: () => _edit(group: group),
            icon: const Icon(Icons.tune_rounded, size: 18),
            label: const Text('Cambiar cuota'),
          ),
          onTap: () => _edit(group: group),
        );
      },
    );
  }
}

/// Crea un grupo ([group] nulo) o cambia su cuota. El nombre no se puede
/// cambiar: la API solo acepta la cuota en `PATCH /groups/{id}`.
class _GroupDialog extends StatefulWidget {
  const _GroupDialog({required this.service, this.group});

  final AdminService service;
  final AdminGroup? group;

  @override
  State<_GroupDialog> createState() => _GroupDialogState();
}

class _GroupDialogState extends State<_GroupDialog> {
  final _name = TextEditingController();
  late QuotaBytes _quota = widget.group?.quotaBytes;
  bool _saving = false;
  String? _error;

  bool get _creating => widget.group == null;

  @override
  void dispose() {
    _name.dispose();
    super.dispose();
  }

  bool get _valid =>
      _quota != -1 && (!_creating || _name.text.trim().isNotEmpty);

  Future<void> _submit() async {
    if (!_valid || _saving) return;
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final group = _creating
          ? await widget.service.createGroup(
              _name.text.trim(),
              quotaBytes: _quota,
            )
          : await widget.service.updateGroupQuota(widget.group!.id, _quota);
      if (mounted) Navigator.of(context).pop(group);
    } on ApiException catch (e) {
      if (mounted) {
        setState(() {
          _saving = false;
          _error = e.message;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: Text(
        _creating ? 'Nuevo grupo' : 'Cuota de «${widget.group!.name}»',
      ),
      content: SizedBox(
        width: 440,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            if (_creating) ...[
              TextField(
                controller: _name,
                autofocus: true,
                decoration: const InputDecoration(labelText: 'Nombre'),
                onChanged: (_) => setState(() {}),
              ),
              const SizedBox(height: 16),
            ],
            Text(
              'Cuota por miembro',
              style: Theme.of(context).textTheme.labelLarge,
            ),
            const SizedBox(height: 8),
            QuotaEditor(
              initial: widget.group?.quotaBytes,
              inheritLabel: 'Sin cuota',
              onChanged: (quota) => setState(() => _quota = quota),
            ),
            if (_error != null) ...[
              const SizedBox(height: 14),
              InlineAlert(message: _error!),
            ],
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: _saving ? null : () => Navigator.of(context).pop(),
          child: const Text('Cancelar'),
        ),
        FilledButton(
          onPressed: _valid && !_saving ? _submit : null,
          child: Text(_creating ? 'Crear grupo' : 'Guardar'),
        ),
      ],
    );
  }
}
