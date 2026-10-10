import 'package:flutter/material.dart';

import '../../../../core/format/formatters.dart';
import '../../../../core/network/api_exception.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../../core/theme/app_theme.dart';
import '../../../../core/widgets/confirm_dialog.dart';
import '../../../../core/widgets/page_scaffold.dart';
import '../../../../core/widgets/view_states.dart';
import '../../data/admin_models.dart';
import '../../data/admin_service.dart';
import '../widgets/admin_feedback.dart';

/// Invitaciones (§78): la alternativa a crear tú la cuenta, para que la
/// otra persona elija su contraseña. El código solo se ve al crearla.
class InvitationsTab extends StatefulWidget {
  const InvitationsTab({super.key, required this.service});

  final AdminService service;

  @override
  State<InvitationsTab> createState() => _InvitationsTabState();
}

class _InvitationsTabState extends State<InvitationsTab> {
  List<AdminInvitation>? _invitations;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final list = await widget.service.listInvitations();
      list.sort((a, b) => b.createdAt.compareTo(a.createdAt));
      if (mounted) setState(() => _invitations = list);
    } on ApiException catch (e) {
      if (mounted) setState(() => _error = e.message);
    }
  }

  Future<void> _create() async {
    final created = await showDialog<CreatedInvitation>(
      context: context,
      builder: (_) => _CreateInvitationDialog(service: widget.service),
    );
    if (created == null || !mounted) return;
    _load();
    await showDialog<void>(
      context: context,
      builder: (_) => _InvitationTokenDialog(created: created),
    );
  }

  Future<void> _revoke(AdminInvitation invitation) async {
    final ok = await showConfirmDialog(
      context,
      title: 'Revocar la invitación',
      message:
          'El código dejará de servir para siempre, aunque le quedaran usos.',
      confirmLabel: 'Revocar',
      danger: true,
    );
    if (!ok || !mounted) return;
    await runAdminAction(
      context,
      () => widget.service.revokeInvitation(invitation.id),
      success: 'Invitación revocada.',
    );
    _load();
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SubToolbar(
          leading: Text(
            'La persona invitada elige su usuario y contraseña al canjear '
            'el código.',
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
              onPressed: _create,
              icon: const Icon(Icons.mail_outline_rounded, size: 18),
              label: const Text('Nueva invitación'),
            ),
          ],
        ),
        Expanded(child: _buildBody(context)),
      ],
    );
  }

  Widget _buildBody(BuildContext context) {
    if (_error != null) return ErrorState(message: _error!, onRetry: _load);
    final list = _invitations;
    if (list == null) return const LoadingState();
    if (list.isEmpty) {
      return const EmptyState(
        icon: Icons.mark_email_unread_outlined,
        title: 'No hay invitaciones',
        message: 'Crea una para que alguien se dé de alta por su cuenta.',
      );
    }
    final p = context.palette;
    return ListView.separated(
      padding: const EdgeInsets.symmetric(vertical: 6),
      itemCount: list.length,
      separatorBuilder: (_, _) => const Divider(height: 1),
      itemBuilder: (context, index) {
        final inv = list[index];
        final role = UserRole.values
            .where((r) => r.id == inv.roleId)
            .map((r) => r.label)
            .firstOrNull;
        final (label, color, background) = inv.revoked
            ? ('Revocada', p.textMuted, p.surfaceMuted)
            : inv.exhausted
            ? ('Agotada', p.textMuted, p.surfaceMuted)
            : inv.expired
            ? ('Caducada', p.warning, p.warningSoft)
            : ('Disponible', p.success, p.successSoft);
        return ListTile(
          contentPadding: const EdgeInsets.symmetric(horizontal: 20),
          leading: Icon(Icons.mail_outline_rounded, color: p.textMuted),
          title: Text(
            '${role ?? 'Usuario'} · ${inv.useCount} de '
            '${pluralize(inv.maxUses, 'uso')}',
          ),
          subtitle: Text(
            'Creada ${formatAgo(inv.createdAt)} · '
            '${inv.expired ? 'caducó' : 'caduca'} el '
            '${formatDateTime(inv.expiresAt)}',
          ),
          trailing: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              StatusPill(label: label, color: color, background: background),
              const SizedBox(width: 8),
              if (inv.usable)
                IconButton(
                  tooltip: 'Revocar',
                  onPressed: () => _revoke(inv),
                  icon: const Icon(Icons.block_rounded),
                ),
            ],
          ),
        );
      },
    );
  }
}

class _CreateInvitationDialog extends StatefulWidget {
  const _CreateInvitationDialog({required this.service});

  final AdminService service;

  @override
  State<_CreateInvitationDialog> createState() =>
      _CreateInvitationDialogState();
}

class _CreateInvitationDialogState extends State<_CreateInvitationDialog> {
  UserRole _role = UserRole.user;
  int _maxUses = 1;
  int _ttlHours = 72;
  bool _saving = false;
  String? _error;

  static const _ttlOptions = [
    (24, '1 día'),
    (72, '3 días'),
    (168, '1 semana'),
    (720, '30 días'),
  ];

  Future<void> _submit() async {
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final created = await widget.service.createInvitation(
        role: _role,
        maxUses: _maxUses,
        ttlHours: _ttlHours,
      );
      if (mounted) Navigator.of(context).pop(created);
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
      title: const Text('Nueva invitación'),
      content: SizedBox(
        width: 420,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            DropdownButtonFormField<UserRole>(
              initialValue: _role,
              decoration: const InputDecoration(labelText: 'Rol de la cuenta'),
              items: [
                for (final role in UserRole.assignable)
                  DropdownMenuItem(value: role, child: Text(role.label)),
              ],
              onChanged: (role) => setState(() => _role = role ?? _role),
            ),
            const SizedBox(height: 12),
            DropdownButtonFormField<int>(
              initialValue: _maxUses,
              decoration: const InputDecoration(labelText: 'Usos'),
              items: [
                for (final n in [1, 5, 10, 25])
                  DropdownMenuItem(
                    value: n,
                    child: Text(n == 1 ? '1 persona' : '$n personas'),
                  ),
              ],
              onChanged: (n) => setState(() => _maxUses = n ?? _maxUses),
            ),
            const SizedBox(height: 12),
            DropdownButtonFormField<int>(
              initialValue: _ttlHours,
              decoration: const InputDecoration(labelText: 'Caduca en'),
              items: [
                for (final (hours, label) in _ttlOptions)
                  DropdownMenuItem(value: hours, child: Text(label)),
              ],
              onChanged: (h) => setState(() => _ttlHours = h ?? _ttlHours),
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
          onPressed: _saving ? null : _submit,
          child: const Text('Crear invitación'),
        ),
      ],
    );
  }
}

class _InvitationTokenDialog extends StatelessWidget {
  const _InvitationTokenDialog({required this.created});

  final CreatedInvitation created;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return AlertDialog(
      title: const Text('Invitación creada'),
      content: SizedBox(
        width: 480,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Text(
              'Copia el código y dáselo a la persona invitada. Por '
              'seguridad solo se muestra ahora: no se puede volver a ver.',
            ),
            const SizedBox(height: 14),
            Container(
              padding: const EdgeInsets.all(12),
              decoration: BoxDecoration(
                color: p.surfaceMuted,
                borderRadius: BorderRadius.circular(8),
                border: Border.all(color: p.border),
              ),
              child: SelectableText(
                created.token,
                style: monoTextStyle.copyWith(fontSize: 13),
              ),
            ),
            const SizedBox(height: 10),
            Text(
              'Caduca el ${formatDateTime(created.invitation.expiresAt)}.',
              style: Theme.of(context).textTheme.bodySmall,
            ),
          ],
        ),
      ),
      actions: [
        TextButton.icon(
          onPressed: () =>
              copySecretToClipboard(context, created.token, what: 'Código'),
          icon: const Icon(Icons.copy_rounded, size: 18),
          label: const Text('Copiar'),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Hecho'),
        ),
      ],
    );
  }
}
