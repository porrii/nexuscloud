import 'package:flutter/material.dart';

import '../../../../core/format/formatters.dart';
import '../../../../core/network/api_exception.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../../core/widgets/page_scaffold.dart';
import '../../../../core/widgets/view_states.dart';
import '../../data/admin_models.dart';
import '../../data/admin_service.dart';

/// Nombre legible e icono de cada tipo de evento
/// (`internal/audit/audit.go`). Uno desconocido se muestra tal cual.
(String, IconData) _describe(String type) => switch (type) {
  'login' => ('Inicio de sesión', Icons.login_rounded),
  'logout' => ('Cierre de sesión', Icons.logout_rounded),
  'login_failed' => ('Inicio de sesión fallido', Icons.gpp_bad_outlined),
  'user_created' => ('Usuario creado', Icons.person_add_alt_rounded),
  'user_disabled' => ('Usuario desactivado', Icons.person_off_outlined),
  'user_deleted' => ('Usuario eliminado', Icons.person_remove_outlined),
  'invitation_created' => ('Invitación creada', Icons.mail_outline_rounded),
  'invitation_revoked' => ('Invitación revocada', Icons.unsubscribe_outlined),
  'upload' => ('Subida', Icons.upload_rounded),
  'download' => ('Descarga', Icons.download_rounded),
  'delete' => ('Borrado', Icons.delete_outline_rounded),
  'move' => ('Movido o renombrado', Icons.drive_file_move_outline),
  'share_create' => ('Compartido', Icons.share_outlined),
  'share_revoke' => ('Compartición retirada', Icons.link_off_rounded),
  'config_changed' => ('Configuración cambiada', Icons.settings_outlined),
  'group_created' => ('Grupo creado', Icons.group_add_outlined),
  'group_member_added' => ('Miembro añadido a un grupo', Icons.groups_outlined),
  'quota_changed' => ('Cuota cambiada', Icons.data_usage_rounded),
  'webauthn_credential_registered' => ('Passkey registrada', Icons.key_rounded),
  'webauthn_credential_revoked' => ('Passkey revocada', Icons.key_off_outlined),
  'webdav_token_created' => ('Token WebDAV creado', Icons.vpn_key_outlined),
  'webdav_token_revoked' => (
    'Token WebDAV revocado',
    Icons.vpn_key_off_outlined,
  ),
  'api_token_created' => ('Token de API creado', Icons.vpn_key_outlined),
  'api_token_revoked' => ('Token de API revocado', Icons.vpn_key_off_outlined),
  'webdav_auth_failed' => ('Acceso WebDAV fallido', Icons.gpp_bad_outlined),
  'favorite_added' => ('Favorito añadido', Icons.star_outline_rounded),
  'favorite_removed' => ('Favorito quitado', Icons.star_border_rounded),
  'anonymous_upload_link_created' => (
    'Enlace de subida creado',
    Icons.drive_folder_upload_outlined,
  ),
  'anonymous_upload_link_revoked' => (
    'Enlace de subida revocado',
    Icons.link_off_rounded,
  ),
  'thumbnail_generation_failed' => (
    'Miniatura fallida',
    Icons.broken_image_outlined,
  ),
  _ => (type, Icons.event_note_outlined),
};

bool _isAlert(String type) =>
    type == 'login_failed' ||
    type == 'webdav_auth_failed' ||
    type == 'thumbnail_generation_failed';

/// Registro de auditoría (§115), del más reciente al más antiguo, por
/// páginas de 100.
class AuditTab extends StatefulWidget {
  const AuditTab({super.key, required this.service});

  final AdminService service;

  @override
  State<AuditTab> createState() => _AuditTabState();
}

class _AuditTabState extends State<AuditTab> {
  static const _pageSize = 100;

  final List<AuditEvent> _events = [];
  Map<String, String> _userNames = const {};
  bool _loading = true;
  bool _hasMore = true;
  bool _onlyAlerts = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _reload();
  }

  Future<void> _reload() async {
    setState(() {
      _events.clear();
      _hasMore = true;
      _error = null;
    });
    try {
      // Los eventos solo traen el id del actor: se traducen a nombres.
      final users = await widget.service.listUsers();
      _userNames = {for (final u in users) u.id: u.username};
    } on ApiException {
      _userNames = const {};
    }
    await _loadMore();
  }

  Future<void> _loadMore() async {
    setState(() => _loading = true);
    try {
      final page = await widget.service.listAuditEvents(
        limit: _pageSize,
        offset: _events.length,
      );
      if (!mounted) return;
      setState(() {
        _events.addAll(page);
        _hasMore = page.length == _pageSize;
        _loading = false;
      });
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e.message;
        _loading = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SubToolbar(
          leading: Row(
            children: [
              FilterChip(
                label: const Text('Solo alertas'),
                selected: _onlyAlerts,
                onSelected: (value) => setState(() => _onlyAlerts = value),
              ),
              const SizedBox(width: 12),
              Flexible(
                child: Text(
                  'Accesos fallidos y errores del servidor',
                  style: Theme.of(context).textTheme.bodySmall,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
            ],
          ),
          actions: [
            IconButton(
              tooltip: 'Actualizar',
              onPressed: _reload,
              icon: const Icon(Icons.refresh_rounded),
            ),
          ],
        ),
        Expanded(child: _buildBody(context)),
      ],
    );
  }

  Widget _buildBody(BuildContext context) {
    if (_error != null && _events.isEmpty) {
      return ErrorState(message: _error!, onRetry: _reload);
    }
    if (_loading && _events.isEmpty) return const LoadingState();
    final events = _onlyAlerts
        ? _events.where((e) => _isAlert(e.eventType)).toList()
        : _events;
    if (events.isEmpty) {
      return const EmptyState(
        icon: Icons.fact_check_outlined,
        title: 'No hay eventos que mostrar',
      );
    }
    return ListView.separated(
      padding: const EdgeInsets.symmetric(vertical: 6),
      itemCount: events.length + (_hasMore ? 1 : 0),
      separatorBuilder: (_, _) => const Divider(height: 1),
      itemBuilder: (context, index) {
        if (index == events.length) {
          return Padding(
            padding: const EdgeInsets.all(12),
            child: Center(
              child: _loading
                  ? const CircularProgressIndicator()
                  : TextButton(
                      onPressed: _loadMore,
                      child: const Text('Cargar más'),
                    ),
            ),
          );
        }
        return _EventRow(event: events[index], userNames: _userNames);
      },
    );
  }
}

class _EventRow extends StatelessWidget {
  const _EventRow({required this.event, required this.userNames});

  final AuditEvent event;
  final Map<String, String> userNames;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    final (label, icon) = _describe(event.eventType);
    final alert = _isAlert(event.eventType);
    final actor = event.actorUserId == null
        ? 'Sin sesión'
        : (userNames[event.actorUserId] ?? 'Usuario eliminado');
    final target = event.targetType == 'user'
        ? userNames[event.targetId]
        : null;
    final details = <String>[
      actor,
      if (target != null && target != actor) '→ $target',
      if (event.metadata['name'] is String) event.metadata['name'] as String,
      if (event.metadata['username'] is String && target == null)
        event.metadata['username'] as String,
      if (event.ip != null) event.ip!,
    ];

    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 10),
      child: Row(
        children: [
          Icon(icon, size: 20, color: alert ? p.danger : p.textMuted),
          const SizedBox(width: 14),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  label,
                  style: text.bodyMedium?.copyWith(
                    fontWeight: FontWeight.w600,
                    color: alert ? p.danger : p.textPrimary,
                  ),
                ),
                const SizedBox(height: 2),
                Text(
                  details.join(' · '),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: text.bodySmall,
                ),
              ],
            ),
          ),
          const SizedBox(width: 12),
          Tooltip(
            message: formatDateTime(event.occurredAt),
            child: Text(formatAgo(event.occurredAt), style: text.bodySmall),
          ),
        ],
      ),
    );
  }
}
