import 'package:flutter/material.dart';

import '../theme/app_palette.dart';

/// Cabecera de sección del área de contenido: título grande, subtítulo
/// opcional y acciones a la derecha. Todas las secciones de la barra
/// lateral la usan para que el salto entre pantallas sea coherente.
class PageHeader extends StatelessWidget {
  const PageHeader({
    super.key,
    required this.title,
    this.subtitle,
    this.actions = const [],
    this.bottom,
    this.padding = const EdgeInsets.fromLTRB(28, 22, 28, 16),
  });

  final String title;
  final String? subtitle;
  final List<Widget> actions;
  final Widget? bottom;
  final EdgeInsets padding;

  @override
  Widget build(BuildContext context) {
    final text = Theme.of(context).textTheme;
    final p = context.palette;
    return Padding(
      padding: padding,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        mainAxisSize: MainAxisSize.min,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.center,
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(title, style: text.headlineSmall),
                    if (subtitle != null) ...[
                      const SizedBox(height: 4),
                      Text(
                        subtitle!,
                        style: text.bodyMedium?.copyWith(color: p.textMuted),
                      ),
                    ],
                  ],
                ),
              ),
              if (actions.isNotEmpty) ...[
                const SizedBox(width: 16),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  crossAxisAlignment: WrapCrossAlignment.center,
                  children: actions,
                ),
              ],
            ],
          ),
          if (bottom != null) ...[const SizedBox(height: 12), bottom!],
        ],
      ),
    );
  }
}

/// Pestañas de una sección (icono + texto en línea), alineadas a la
/// izquierda bajo la cabecera. Necesita un `DefaultTabController` encima.
class SectionTabs extends StatelessWidget {
  const SectionTabs({super.key, required this.tabs});

  final List<(IconData, String)> tabs;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 16),
      child: DecoratedBox(
        decoration: BoxDecoration(
          border: Border(bottom: BorderSide(color: context.palette.border)),
        ),
        child: TabBar(
          isScrollable: true,
          dividerHeight: 0,
          labelPadding: const EdgeInsets.symmetric(horizontal: 12),
          tabs: [
            for (final (icon, label) in tabs)
              Tab(
                height: 44,
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Icon(icon, size: 18),
                    const SizedBox(width: 8),
                    Text(label),
                  ],
                ),
              ),
          ],
        ),
      ),
    );
  }
}

/// Barra de herramientas compacta para las vistas que viven dentro de una
/// pestaña (sin título grande propio): texto de contexto a la izquierda y
/// acciones a la derecha, con una línea inferior.
class SubToolbar extends StatelessWidget {
  const SubToolbar({super.key, required this.leading, this.actions = const []});

  final Widget leading;
  final List<Widget> actions;

  @override
  Widget build(BuildContext context) {
    return Container(
      constraints: const BoxConstraints(minHeight: 52),
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 6),
      decoration: BoxDecoration(
        color: context.palette.surface,
        border: Border(bottom: BorderSide(color: context.palette.border)),
      ),
      child: Row(
        children: [
          Expanded(child: leading),
          ...actions,
        ],
      ),
    );
  }
}

/// Tarjeta de sección (ajustes, formularios): título con icono opcional,
/// descripción y contenido.
class SectionCard extends StatelessWidget {
  const SectionCard({
    super.key,
    required this.title,
    required this.child,
    this.icon,
    this.description,
    this.trailing,
    this.padding = const EdgeInsets.fromLTRB(20, 18, 20, 20),
  });

  final String title;
  final String? description;
  final IconData? icon;
  final Widget? trailing;
  final Widget child;
  final EdgeInsets padding;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final text = Theme.of(context).textTheme;
    return Card(
      child: Padding(
        padding: padding,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                if (icon != null) ...[
                  Container(
                    width: 32,
                    height: 32,
                    decoration: BoxDecoration(
                      color: p.accentSoft,
                      borderRadius: BorderRadius.circular(8),
                    ),
                    child: Icon(icon, size: 18, color: p.accent),
                  ),
                  const SizedBox(width: 12),
                ],
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(title, style: text.titleMedium),
                      if (description != null) ...[
                        const SizedBox(height: 2),
                        Text(description!, style: text.bodySmall),
                      ],
                    ],
                  ),
                ),
                ?trailing,
              ],
            ),
            const SizedBox(height: 14),
            child,
          ],
        ),
      ),
    );
  }
}

/// Fila de ajuste con interruptor: título, explicación y `Switch`. Más
/// compacta y legible que un `SwitchListTile` con subtítulo largo.
class SettingSwitchRow extends StatelessWidget {
  const SettingSwitchRow({
    super.key,
    required this.title,
    required this.value,
    required this.onChanged,
    this.description,
  });

  final String title;
  final String? description;
  final bool value;
  final ValueChanged<bool>? onChanged;

  @override
  Widget build(BuildContext context) {
    final text = Theme.of(context).textTheme;
    return MergeSemantics(
      child: InkWell(
        borderRadius: BorderRadius.circular(8),
        onTap: onChanged == null ? null : () => onChanged!(!value),
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 10, horizontal: 4),
          child: Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      title,
                      style: text.bodyLarge?.copyWith(
                        fontWeight: FontWeight.w500,
                      ),
                    ),
                    if (description != null) ...[
                      const SizedBox(height: 2),
                      Text(description!, style: text.bodySmall),
                    ],
                  ],
                ),
              ),
              const SizedBox(width: 16),
              Switch(value: value, onChanged: onChanged),
            ],
          ),
        ),
      ),
    );
  }
}

/// Pastilla de estado pequeña (`Ambos`, `Auto`, `Con contraseña`...).
class StatusPill extends StatelessWidget {
  const StatusPill({
    super.key,
    required this.label,
    this.icon,
    this.color,
    this.background,
  });

  final String label;
  final IconData? icon;
  final Color? color;
  final Color? background;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final fg = color ?? p.textSecondary;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: background ?? p.surfaceMuted,
        borderRadius: BorderRadius.circular(999),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (icon != null) ...[
            Icon(icon, size: 13, color: fg),
            const SizedBox(width: 4),
          ],
          Text(
            label,
            style: TextStyle(
              fontSize: 12,
              fontWeight: FontWeight.w500,
              color: fg,
            ),
          ),
        ],
      ),
    );
  }
}
