import 'dart:math';

import 'package:flutter/material.dart';

import '../../../../core/format/formatters.dart';
import '../../../../core/network/api_exception.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../../core/widgets/confirm_dialog.dart';
import '../../../../core/widgets/page_scaffold.dart';
import '../../../../core/widgets/view_states.dart';
import '../../../files/presentation/widgets/browser_chrome.dart'
    show contextMenuItem;
import '../../../shell/presentation/widgets/sidebar.dart' show UserAvatar;
import '../../data/admin_models.dart';
import '../../data/admin_service.dart';
import '../widgets/admin_feedback.dart';
import '../widgets/quota_editor.dart';

part 'user_dialogs.dart';

enum _UserAction { edit, toggleActive, addToGroup, delete }

class UsersTab extends StatefulWidget {
  const UsersTab({super.key, required this.service, this.currentUserId});

  final AdminService service;

  /// Tu propia cuenta: no se puede desactivar ni eliminar desde aquí.
  final String? currentUserId;

  @override
  State<UsersTab> createState() => _UsersTabState();
}

class _UsersTabState extends State<UsersTab> {
  List<AdminUser>? _users;
  String? _error;
  final _filter = TextEditingController();

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _filter.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final users = await widget.service.listUsers();
      users.sort(
        (a, b) => a.label.toLowerCase().compareTo(b.label.toLowerCase()),
      );
      if (mounted) setState(() => _users = users);
    } on ApiException catch (e) {
      if (mounted) setState(() => _error = e.message);
    }
  }

  List<AdminUser> get _visible {
    final needle = _filter.text.trim().toLowerCase();
    final users = _users ?? const [];
    if (needle.isEmpty) return users;
    return users
        .where(
          (u) =>
              u.username.toLowerCase().contains(needle) ||
              u.displayName.toLowerCase().contains(needle) ||
              (u.email ?? '').toLowerCase().contains(needle),
        )
        .toList();
  }

  Future<void> _create() async {
    final created = await showDialog<AdminUser>(
      context: context,
      builder: (_) => _CreateUserDialog(service: widget.service),
    );
    if (created == null || !mounted) return;
    showAdminToast(context, 'Usuario «${created.username}» creado.');
    _load();
  }

  Future<void> _edit(AdminUser user) async {
    final updated = await showDialog<AdminUser>(
      context: context,
      builder: (_) => _EditUserDialog(service: widget.service, user: user),
    );
    if (updated == null || !mounted) return;
    showAdminToast(context, 'Cambios guardados.');
    _load();
  }

  Future<void> _toggleActive(AdminUser user) async {
    final disabling = user.active;
    if (disabling) {
      final ok = await showConfirmDialog(
        context,
        title: 'Desactivar a ${user.label}',
        message:
            'No podrá iniciar sesión: se cierran sus sesiones y dejan de '
            'funcionar sus tokens de API y WebDAV. Lo que compartió con '
            'usuarios y grupos sigue accesible y sus archivos se conservan. '
            'Reactivarla lo restaura todo.\n\nSus enlaces públicos y de '
            'subida anónima también dejan de funcionar si el servidor está '
            'actualizado; las versiones anteriores los mantenían activos '
            '(si dudas, revócalos con la CLI: shares revoke).',
        confirmLabel: 'Desactivar',
        danger: true,
      );
      if (!ok || !mounted) return;
    }
    await runAdminAction(
      context,
      () => widget.service.updateUser(user.id, active: !disabling),
      success: disabling ? 'Cuenta desactivada.' : 'Cuenta activada.',
    );
    _load();
  }

  Future<void> _addToGroup(AdminUser user) async {
    final added = await showDialog<String>(
      context: context,
      builder: (_) => _AddToGroupDialog(service: widget.service, user: user),
    );
    if (added != null && mounted) {
      showAdminToast(context, '${user.label} añadido a «$added».');
    }
  }

  Future<void> _delete(AdminUser user) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (_) => _DeleteUserDialog(user: user),
    );
    if (ok != true || !mounted) return;
    await runAdminAction(
      context,
      () => widget.service.deleteUser(user.id),
      success: 'Usuario «${user.username}» eliminado.',
    );
    _load();
  }

  Future<void> _showMenu(AdminUser user, Offset position) async {
    final isSelf = user.id == widget.currentUserId;
    final overlay = Overlay.of(context).context.findRenderObject() as RenderBox;
    final action = await showMenu<_UserAction>(
      context: context,
      position: RelativeRect.fromRect(
        position & const Size(1, 1),
        Offset.zero & overlay.size,
      ),
      constraints: const BoxConstraints(minWidth: 220),
      items: [
        contextMenuItem(_UserAction.edit, Icons.edit_outlined, 'Editar'),
        contextMenuItem(
          _UserAction.addToGroup,
          Icons.group_add_outlined,
          'Añadir a un grupo',
        ),
        if (!isSelf) ...[
          const PopupMenuDivider(height: 8),
          contextMenuItem(
            _UserAction.toggleActive,
            user.active
                ? Icons.block_rounded
                : Icons.check_circle_outline_rounded,
            user.active ? 'Desactivar' : 'Activar',
          ),
          contextMenuItem(
            _UserAction.delete,
            Icons.delete_outline_rounded,
            'Eliminar',
            danger: true,
          ),
        ],
      ],
    );
    if (action == null || !mounted) return;
    switch (action) {
      case _UserAction.edit:
        _edit(user);
      case _UserAction.toggleActive:
        _toggleActive(user);
      case _UserAction.addToGroup:
        _addToGroup(user);
      case _UserAction.delete:
        _delete(user);
    }
  }

  @override
  Widget build(BuildContext context) {
    final users = _users;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SubToolbar(
          leading: Row(
            children: [
              SizedBox(
                width: 260,
                child: TextField(
                  controller: _filter,
                  onChanged: (_) => setState(() {}),
                  style: const TextStyle(fontSize: 13.5),
                  decoration: const InputDecoration(
                    hintText: 'Buscar por nombre o correo',
                    isDense: true,
                    prefixIcon: Icon(Icons.search_rounded, size: 18),
                  ),
                ),
              ),
              const SizedBox(width: 12),
              if (users != null)
                Flexible(
                  child: Text(
                    pluralize(users.length, 'usuario'),
                    style: Theme.of(context).textTheme.bodySmall,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
            ],
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
              icon: const Icon(Icons.person_add_alt_rounded, size: 18),
              label: const Text('Nuevo usuario'),
            ),
          ],
        ),
        Expanded(child: _buildBody()),
      ],
    );
  }

  Widget _buildBody() {
    if (_error != null) return ErrorState(message: _error!, onRetry: _load);
    if (_users == null) return const LoadingState();
    final visible = _visible;
    if (visible.isEmpty) {
      return const EmptyState(
        icon: Icons.person_search_outlined,
        title: 'Ningún usuario coincide',
      );
    }
    return ListView.separated(
      padding: const EdgeInsets.symmetric(vertical: 6),
      itemCount: visible.length,
      separatorBuilder: (_, _) => const Divider(height: 1),
      itemBuilder: (context, index) {
        final user = visible[index];
        return _UserRow(
          key: ValueKey('user-${user.id}'),
          user: user,
          isSelf: user.id == widget.currentUserId,
          onMenu: (position) => _showMenu(user, position),
          onOpen: () => _edit(user),
        );
      },
    );
  }
}

class _UserRow extends StatelessWidget {
  const _UserRow({
    super.key,
    required this.user,
    required this.isSelf,
    required this.onMenu,
    required this.onOpen,
  });

  final AdminUser user;
  final bool isSelf;
  final ValueChanged<Offset> onMenu;
  final VoidCallback onOpen;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    final subtitle = [
      '@${user.username}',
      if (user.email != null) user.email!,
    ].join(' · ');

    return InkWell(
      onTap: onOpen,
      onSecondaryTapUp: (d) => onMenu(d.globalPosition),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 10),
        child: Row(
          children: [
            Opacity(
              opacity: user.active ? 1 : 0.45,
              child: UserAvatar(name: user.label, size: 36),
            ),
            const SizedBox(width: 14),
            Expanded(
              flex: 3,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Flexible(
                        child: Text(
                          user.label,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: text.bodyMedium?.copyWith(
                            fontWeight: FontWeight.w600,
                            color: p.textPrimary,
                          ),
                        ),
                      ),
                      if (isSelf) ...[
                        const SizedBox(width: 8),
                        StatusPill(
                          label: 'Tú',
                          color: p.accentOnSoft,
                          background: p.accentSoft,
                        ),
                      ],
                    ],
                  ),
                  const SizedBox(height: 2),
                  Text(
                    subtitle,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: text.bodySmall,
                  ),
                ],
              ),
            ),
            const SizedBox(width: 12),
            Expanded(
              flex: 2,
              child: Wrap(
                spacing: 6,
                runSpacing: 4,
                children: [
                  user.active
                      ? StatusPill(
                          label: 'Activo',
                          color: p.success,
                          background: p.successSoft,
                        )
                      : StatusPill(
                          label: 'Desactivado',
                          color: p.danger,
                          background: p.dangerSoft,
                        ),
                  if (user.hasTotp)
                    StatusPill(
                      label: '2FA',
                      icon: Icons.verified_user_outlined,
                      color: p.accentOnSoft,
                      background: p.accentSoft,
                    ),
                ],
              ),
            ),
            Expanded(
              flex: 2,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    'Cuota: ${describeQuota(user.quotaBytes, inheritLabel: 'heredada')}',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: text.bodySmall?.copyWith(color: p.textSecondary),
                  ),
                  Text(
                    user.lastLoginAt == null
                        ? 'Nunca ha iniciado sesión'
                        : 'Último acceso ${formatAgo(user.lastLoginAt!)}',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: text.bodySmall,
                  ),
                ],
              ),
            ),
            Builder(
              builder: (context) => IconButton(
                tooltip: 'Más acciones',
                icon: const Icon(Icons.more_horiz_rounded),
                onPressed: () {
                  final box = context.findRenderObject() as RenderBox;
                  onMenu(box.localToGlobal(Offset(0, box.size.height)));
                },
              ),
            ),
          ],
        ),
      ),
    );
  }
}
