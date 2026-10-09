import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../../../core/format/formatters.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../account/data/quota_service.dart';
import '../../../auth/domain/entities/app_user.dart';
import '../../../sync/presentation/sync_activity.dart';

enum ShellSection { files, shared, sync, trash, settings, admin, search }

/// Barra lateral fija: marca, buscador global, navegación, estado de
/// sincronización, espacio usado y usuario. Mismo esquema que el
/// `AppShell.tsx` de la web.
class Sidebar extends StatelessWidget {
  const Sidebar({
    super.key,
    required this.current,
    required this.onSelect,
    required this.onSearch,
    required this.searchController,
    required this.searchFocus,
    required this.syncActivity,
    required this.quota,
    required this.onRefreshQuota,
    required this.user,
    required this.updateAvailable,
    required this.onLogout,
    this.isAdmin = false,
  });

  final ShellSection current;
  final ValueChanged<ShellSection> onSelect;
  final ValueChanged<String> onSearch;
  final TextEditingController searchController;
  final FocusNode searchFocus;
  final SyncActivity? syncActivity;
  final StorageQuota? quota;
  final VoidCallback onRefreshQuota;
  final AppUser? user;
  final bool updateAvailable;
  final VoidCallback onLogout;

  /// Muestra «Administración» (solo lo decide la interfaz: el servidor
  /// vuelve a comprobar el rol en cada petición).
  final bool isAdmin;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return Container(
      width: 252,
      decoration: BoxDecoration(
        color: p.sidebar,
        border: Border(right: BorderSide(color: p.border)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const Padding(
            padding: EdgeInsets.fromLTRB(18, 20, 18, 16),
            child: BrandMark(),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(12, 0, 12, 12),
            child: _SearchField(
              controller: searchController,
              focusNode: searchFocus,
              onSubmit: onSearch,
            ),
          ),
          _NavItem(
            icon: Icons.folder_outlined,
            activeIcon: Icons.folder_rounded,
            label: 'Mis archivos',
            shortcut: 'Ctrl+1',
            selected: current == ShellSection.files,
            onTap: () => onSelect(ShellSection.files),
          ),
          _NavItem(
            icon: Icons.people_outline_rounded,
            activeIcon: Icons.people_rounded,
            label: 'Compartido',
            shortcut: 'Ctrl+2',
            selected: current == ShellSection.shared,
            onTap: () => onSelect(ShellSection.shared),
          ),
          _NavItem(
            icon: Icons.sync_rounded,
            activeIcon: Icons.sync_rounded,
            label: 'Sincronización',
            shortcut: 'Ctrl+3',
            selected: current == ShellSection.sync,
            onTap: () => onSelect(ShellSection.sync),
            trailing: syncActivity == null
                ? null
                : _SyncDot(activity: syncActivity!),
          ),
          _NavItem(
            icon: Icons.delete_outline_rounded,
            activeIcon: Icons.delete_rounded,
            label: 'Papelera',
            shortcut: 'Ctrl+4',
            selected: current == ShellSection.trash,
            onTap: () => onSelect(ShellSection.trash),
          ),
          _NavItem(
            icon: Icons.settings_outlined,
            activeIcon: Icons.settings_rounded,
            label: 'Ajustes',
            shortcut: 'Ctrl+5',
            selected: current == ShellSection.settings,
            onTap: () => onSelect(ShellSection.settings),
            trailing: updateAvailable
                ? Tooltip(
                    message: 'Hay una actualización disponible',
                    child: Container(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 6,
                        vertical: 1,
                      ),
                      decoration: BoxDecoration(
                        color: p.accent,
                        borderRadius: BorderRadius.circular(999),
                      ),
                      child: const Text(
                        'Nueva',
                        style: TextStyle(
                          color: Colors.white,
                          fontSize: 10.5,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                    ),
                  )
                : null,
          ),
          if (isAdmin)
            _NavItem(
              icon: Icons.admin_panel_settings_outlined,
              activeIcon: Icons.admin_panel_settings_rounded,
              label: 'Administración',
              shortcut: 'Ctrl+6',
              selected: current == ShellSection.admin,
              onTap: () => onSelect(ShellSection.admin),
            ),
          const Spacer(),
          if (syncActivity != null)
            Padding(
              padding: const EdgeInsets.fromLTRB(12, 0, 12, 8),
              child: SyncStatusCard(
                activity: syncActivity!,
                onTap: () => onSelect(ShellSection.sync),
              ),
            ),
          if (quota != null)
            Padding(
              padding: const EdgeInsets.fromLTRB(12, 0, 12, 8),
              child: _UsageMeter(quota: quota!, onTap: onRefreshQuota),
            ),
          Divider(height: 1, color: p.border),
          _UserFooter(user: user, onLogout: onLogout),
        ],
      ),
    );
  }
}

/// Logotipo: cuadrado con degradado azul-índigo y una nube, más el nombre.
class BrandMark extends StatelessWidget {
  const BrandMark({super.key, this.size = 32, this.showName = true});

  final double size;
  final bool showName;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final mark = Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        gradient: const LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [Tw.blue500, Tw.indigo700],
        ),
        borderRadius: BorderRadius.circular(size * 0.28),
        boxShadow: [
          BoxShadow(
            color: Tw.blue600.withValues(alpha: 0.28),
            blurRadius: size * 0.35,
            offset: Offset(0, size * 0.1),
          ),
        ],
      ),
      child: Icon(Icons.cloud_rounded, color: Colors.white, size: size * 0.58),
    );
    if (!showName) return mark;
    return Row(
      children: [
        mark,
        const SizedBox(width: 10),
        Text(
          'NexusCloud',
          style: TextStyle(
            fontSize: size * 0.53,
            fontWeight: FontWeight.w700,
            letterSpacing: -0.3,
            color: p.textPrimary,
          ),
        ),
      ],
    );
  }
}

class _SearchField extends StatelessWidget {
  const _SearchField({
    required this.controller,
    required this.focusNode,
    required this.onSubmit,
  });

  final TextEditingController controller;
  final FocusNode focusNode;
  final ValueChanged<String> onSubmit;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return CallbackShortcuts(
      bindings: {
        const SingleActivator(LogicalKeyboardKey.escape): () {
          controller.clear();
          focusNode.unfocus();
        },
      },
      child: TextField(
        controller: controller,
        focusNode: focusNode,
        textInputAction: TextInputAction.search,
        onSubmitted: onSubmit,
        style: const TextStyle(fontSize: 13.5),
        decoration: InputDecoration(
          hintText: 'Buscar en todo…',
          filled: true,
          fillColor: p.surfaceMuted.withValues(alpha: 0.7),
          contentPadding: const EdgeInsets.symmetric(
            vertical: 10,
            horizontal: 12,
          ),
          prefixIcon: const Icon(Icons.search_rounded, size: 18),
          prefixIconConstraints: const BoxConstraints(minWidth: 38),
          suffixIcon: Padding(
            padding: const EdgeInsets.only(right: 8),
            child: Text(
              'Ctrl+K',
              style: TextStyle(fontSize: 11, color: p.textMuted),
            ),
          ),
          suffixIconConstraints: const BoxConstraints(
            minHeight: 0,
            minWidth: 0,
          ),
          enabledBorder: OutlineInputBorder(
            borderRadius: BorderRadius.circular(8),
            borderSide: BorderSide(color: p.border),
          ),
        ),
      ),
    );
  }
}

class _NavItem extends StatelessWidget {
  const _NavItem({
    required this.icon,
    required this.activeIcon,
    required this.label,
    required this.selected,
    required this.onTap,
    this.shortcut,
    this.trailing,
  });

  final IconData icon;
  final IconData activeIcon;
  final String label;
  final bool selected;
  final VoidCallback onTap;
  final String? shortcut;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final fg = selected ? p.accentOnSoft : p.textSecondary;
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 1.5),
      child: Semantics(
        selected: selected,
        button: true,
        child: Tooltip(
          message: shortcut == null ? label : '$label ($shortcut)',
          waitDuration: const Duration(milliseconds: 900),
          child: Material(
            color: selected ? p.accentSoft : Colors.transparent,
            borderRadius: BorderRadius.circular(8),
            child: InkWell(
              borderRadius: BorderRadius.circular(8),
              onTap: onTap,
              child: SizedBox(
                height: 38,
                child: Row(
                  children: [
                    const SizedBox(width: 10),
                    Icon(
                      selected ? activeIcon : icon,
                      size: 20,
                      color: selected ? p.accent : p.textMuted,
                    ),
                    const SizedBox(width: 12),
                    Expanded(
                      child: Text(
                        label,
                        style: TextStyle(
                          fontSize: 14,
                          fontWeight: selected
                              ? FontWeight.w600
                              : FontWeight.w500,
                          color: fg,
                        ),
                      ),
                    ),
                    if (trailing != null) ...[
                      trailing!,
                      const SizedBox(width: 10),
                    ],
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

({Color color, IconData icon, String title}) _syncVisuals(
  SyncHealth health,
  AppPalette p,
) {
  return switch (health) {
    SyncHealth.idle => (
      color: p.textMuted,
      icon: Icons.cloud_off_outlined,
      title: 'Sin carpetas vinculadas',
    ),
    SyncHealth.running => (
      color: p.accent,
      icon: Icons.sync_rounded,
      title: 'Sincronizando…',
    ),
    SyncHealth.ok => (
      color: p.success,
      icon: Icons.cloud_done_outlined,
      title: 'Todo sincronizado',
    ),
    SyncHealth.attention => (
      color: p.warning,
      icon: Icons.pending_actions_rounded,
      title: 'Requiere tu revisión',
    ),
    SyncHealth.error => (
      color: p.danger,
      icon: Icons.sync_problem_rounded,
      title: 'Sincronización con errores',
    ),
  };
}

class _SyncDot extends StatelessWidget {
  const _SyncDot({required this.activity});

  final SyncActivity activity;

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: activity,
      builder: (context, _) {
        final health = activity.health;
        if (health == SyncHealth.idle || health == SyncHealth.ok) {
          return const SizedBox.shrink();
        }
        final v = _syncVisuals(health, context.palette);
        return Container(
          width: 8,
          height: 8,
          decoration: BoxDecoration(color: v.color, shape: BoxShape.circle),
        );
      },
    );
  }
}

/// Tarjeta de estado de sincronización (siempre visible en la barra
/// lateral): antes solo se veía entrando en la página de ajustes.
class SyncStatusCard extends StatelessWidget {
  const SyncStatusCard({
    super.key,
    required this.activity,
    required this.onTap,
  });

  final SyncActivity activity;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: activity,
      builder: (context, _) {
        final p = context.palette;
        final health = activity.health;
        final v = _syncVisuals(health, p);
        final String subtitle;
        if (health == SyncHealth.running) {
          subtitle = activity.statusMessage ?? 'Comparando cambios…';
        } else if (health == SyncHealth.idle) {
          subtitle = 'Vincula una carpeta local';
        } else if (activity.lastFinishedAt != null) {
          subtitle = 'Última: ${formatAgo(activity.lastFinishedAt!)}';
        } else {
          subtitle = 'Aún no se ha sincronizado';
        }
        return Material(
          color: p.surfaceMuted.withValues(alpha: 0.55),
          borderRadius: BorderRadius.circular(10),
          child: InkWell(
            borderRadius: BorderRadius.circular(10),
            onTap: onTap,
            child: Padding(
              padding: const EdgeInsets.all(10),
              child: Row(
                children: [
                  Container(
                    width: 30,
                    height: 30,
                    decoration: BoxDecoration(
                      color: v.color.withValues(alpha: 0.14),
                      borderRadius: BorderRadius.circular(8),
                    ),
                    child: health == SyncHealth.running
                        ? Padding(
                            padding: const EdgeInsets.all(8),
                            child: CircularProgressIndicator(
                              strokeWidth: 2,
                              color: v.color,
                            ),
                          )
                        : Icon(v.icon, size: 17, color: v.color),
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          v.title,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(
                            fontSize: 13,
                            fontWeight: FontWeight.w600,
                            color: p.textPrimary,
                          ),
                        ),
                        const SizedBox(height: 1),
                        Text(
                          subtitle,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(fontSize: 11.5, color: p.textMuted),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ),
        );
      },
    );
  }
}

class _UsageMeter extends StatelessWidget {
  const _UsageMeter({required this.quota, required this.onTap});

  final StorageQuota quota;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final fraction = quota.fraction;
    final limit = quota.limitBytes;
    final color = fraction == null
        ? p.accent
        : fraction >= 0.95
        ? p.danger
        : fraction >= 0.8
        ? p.warning
        : p.accent;
    return Tooltip(
      message:
          'Archivos ${formatBytes(quota.filesBytes)} · '
          'Papelera ${formatBytes(quota.trashBytes)} · '
          'Versiones ${formatBytes(quota.versionsBytes)}',
      child: InkWell(
        borderRadius: BorderRadius.circular(8),
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 6),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Icon(Icons.storage_rounded, size: 14, color: p.textMuted),
                  const SizedBox(width: 6),
                  Text(
                    'Almacenamiento',
                    style: TextStyle(
                      fontSize: 12,
                      color: p.textSecondary,
                      fontWeight: FontWeight.w500,
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 8),
              ClipRRect(
                borderRadius: BorderRadius.circular(4),
                child: LinearProgressIndicator(
                  value: fraction ?? 0,
                  minHeight: 5,
                  color: color,
                  backgroundColor: p.surfaceMuted,
                ),
              ),
              const SizedBox(height: 6),
              Text(
                limit == null
                    ? '${formatBytes(quota.usedBytes)} usados · sin límite'
                    : '${formatBytes(quota.usedBytes)} de ${formatBytes(limit)}',
                style: TextStyle(fontSize: 11.5, color: p.textMuted),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _UserFooter extends StatelessWidget {
  const _UserFooter({required this.user, required this.onLogout});

  final AppUser? user;
  final VoidCallback onLogout;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final name = user?.displayName.trim().isNotEmpty == true
        ? user!.displayName
        : (user?.username ?? '');
    return Padding(
      padding: const EdgeInsets.fromLTRB(14, 10, 8, 12),
      child: Row(
        children: [
          UserAvatar(name: name),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  name,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(
                    fontSize: 13.5,
                    fontWeight: FontWeight.w600,
                    color: p.textPrimary,
                  ),
                ),
                if (user != null)
                  Text(
                    '@${user!.username}',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 12, color: p.textMuted),
                  ),
              ],
            ),
          ),
          IconButton(
            tooltip: 'Cerrar sesión',
            icon: const Icon(Icons.logout_rounded, size: 19),
            onPressed: onLogout,
          ),
        ],
      ),
    );
  }
}

/// Círculo con las iniciales del usuario sobre un color derivado del nombre.
class UserAvatar extends StatelessWidget {
  const UserAvatar({super.key, required this.name, this.size = 34});

  final String name;
  final double size;

  static const _colors = [
    Color(0xFF2563EB),
    Color(0xFF7C3AED),
    Color(0xFFDB2777),
    Color(0xFF059669),
    Color(0xFFD97706),
    Color(0xFF0891B2),
  ];

  @override
  Widget build(BuildContext context) {
    final parts = name
        .trim()
        .split(RegExp(r'\s+'))
        .where((s) => s.isNotEmpty)
        .toList();
    final initials = parts.isEmpty
        ? '?'
        : parts.length == 1
        ? parts.first.characters.take(2).toString().toUpperCase()
        : '${parts.first.characters.first}${parts.last.characters.first}'
              .toUpperCase();
    // Suma de códigos en vez de `hashCode`: el mismo nombre debe dar
    // siempre el mismo color, en cada arranque.
    final color =
        _colors[name.codeUnits.fold<int>(0, (a, b) => a + b) % _colors.length];
    return Container(
      width: size,
      height: size,
      alignment: Alignment.center,
      decoration: BoxDecoration(color: color, shape: BoxShape.circle),
      child: Text(
        initials,
        style: TextStyle(
          color: Colors.white,
          fontWeight: FontWeight.w600,
          fontSize: size * 0.38,
        ),
      ),
    );
  }
}
