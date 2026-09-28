import 'package:flutter/material.dart';

/// Escala de colores de la familia NexusCloud -- los mismos valores que
/// Tailwind usa en la web (`slate` para neutros, `blue` como acento), para
/// que el cliente de escritorio y la web se sientan un mismo producto.
abstract final class Tw {
  static const slate50 = Color(0xFFF8FAFC);
  static const slate100 = Color(0xFFF1F5F9);
  static const slate200 = Color(0xFFE2E8F0);
  static const slate300 = Color(0xFFCBD5E1);
  static const slate400 = Color(0xFF94A3B8);
  static const slate500 = Color(0xFF64748B);
  static const slate600 = Color(0xFF475569);
  static const slate700 = Color(0xFF334155);
  static const slate800 = Color(0xFF1E293B);
  static const slate900 = Color(0xFF0F172A);
  static const slate950 = Color(0xFF020617);

  static const blue50 = Color(0xFFEFF6FF);
  static const blue100 = Color(0xFFDBEAFE);
  static const blue300 = Color(0xFF93C5FD);
  static const blue400 = Color(0xFF60A5FA);
  static const blue500 = Color(0xFF3B82F6);
  static const blue600 = Color(0xFF2563EB);
  static const blue700 = Color(0xFF1D4ED8);
  static const blue900 = Color(0xFF1E3A8A);
  static const blue950 = Color(0xFF172554);

  static const indigo500 = Color(0xFF6366F1);
  static const indigo700 = Color(0xFF4338CA);

  static const emerald50 = Color(0xFFECFDF5);
  static const emerald400 = Color(0xFF34D399);
  static const emerald600 = Color(0xFF059669);
  static const emerald950 = Color(0xFF022C22);

  static const amber50 = Color(0xFFFFFBEB);
  static const amber400 = Color(0xFFFBBF24);
  static const amber600 = Color(0xFFD97706);
  static const amber950 = Color(0xFF451A03);

  static const red50 = Color(0xFFFEF2F2);
  static const red400 = Color(0xFFF87171);
  static const red600 = Color(0xFFDC2626);
  static const red950 = Color(0xFF450A0A);
}

/// Colores semánticos que `ColorScheme` no cubre bien (superficie de la
/// barra lateral, texto atenuado, fondos suaves de estado...). Se leen con
/// `context.palette` en vez de repetir literales en cada pantalla.
@immutable
class AppPalette extends ThemeExtension<AppPalette> {
  const AppPalette({
    required this.canvas,
    required this.sidebar,
    required this.surface,
    required this.surfaceMuted,
    required this.border,
    required this.borderStrong,
    required this.textPrimary,
    required this.textSecondary,
    required this.textMuted,
    required this.accent,
    required this.accentSoft,
    required this.accentOnSoft,
    required this.success,
    required this.successSoft,
    required this.warning,
    required this.warningSoft,
    required this.danger,
    required this.dangerSoft,
  });

  /// Fondo del área de contenido.
  final Color canvas;
  final Color sidebar;

  /// Tarjetas, listas, diálogos.
  final Color surface;

  /// Hover, cabeceras de tabla, campos de búsqueda.
  final Color surfaceMuted;
  final Color border;
  final Color borderStrong;
  final Color textPrimary;
  final Color textSecondary;
  final Color textMuted;
  final Color accent;

  /// Fondo de un elemento seleccionado (fila, entrada de navegación).
  final Color accentSoft;
  final Color accentOnSoft;
  final Color success;
  final Color successSoft;
  final Color warning;
  final Color warningSoft;
  final Color danger;
  final Color dangerSoft;

  static const light = AppPalette(
    canvas: Tw.slate50,
    sidebar: Colors.white,
    surface: Colors.white,
    surfaceMuted: Tw.slate100,
    border: Tw.slate200,
    borderStrong: Tw.slate300,
    textPrimary: Tw.slate900,
    textSecondary: Tw.slate600,
    textMuted: Tw.slate500,
    accent: Tw.blue600,
    accentSoft: Tw.blue50,
    accentOnSoft: Tw.blue700,
    success: Tw.emerald600,
    successSoft: Tw.emerald50,
    warning: Tw.amber600,
    warningSoft: Tw.amber50,
    danger: Tw.red600,
    dangerSoft: Tw.red50,
  );

  static const dark = AppPalette(
    canvas: Tw.slate950,
    sidebar: Tw.slate900,
    surface: Tw.slate900,
    surfaceMuted: Tw.slate800,
    border: Tw.slate800,
    borderStrong: Tw.slate700,
    textPrimary: Tw.slate50,
    textSecondary: Tw.slate300,
    textMuted: Tw.slate400,
    accent: Tw.blue500,
    accentSoft: Tw.blue950,
    accentOnSoft: Tw.blue300,
    success: Tw.emerald400,
    successSoft: Tw.emerald950,
    warning: Tw.amber400,
    warningSoft: Tw.amber950,
    danger: Tw.red400,
    dangerSoft: Tw.red950,
  );

  @override
  AppPalette copyWith({
    Color? canvas,
    Color? sidebar,
    Color? surface,
    Color? surfaceMuted,
    Color? border,
    Color? borderStrong,
    Color? textPrimary,
    Color? textSecondary,
    Color? textMuted,
    Color? accent,
    Color? accentSoft,
    Color? accentOnSoft,
    Color? success,
    Color? successSoft,
    Color? warning,
    Color? warningSoft,
    Color? danger,
    Color? dangerSoft,
  }) {
    return AppPalette(
      canvas: canvas ?? this.canvas,
      sidebar: sidebar ?? this.sidebar,
      surface: surface ?? this.surface,
      surfaceMuted: surfaceMuted ?? this.surfaceMuted,
      border: border ?? this.border,
      borderStrong: borderStrong ?? this.borderStrong,
      textPrimary: textPrimary ?? this.textPrimary,
      textSecondary: textSecondary ?? this.textSecondary,
      textMuted: textMuted ?? this.textMuted,
      accent: accent ?? this.accent,
      accentSoft: accentSoft ?? this.accentSoft,
      accentOnSoft: accentOnSoft ?? this.accentOnSoft,
      success: success ?? this.success,
      successSoft: successSoft ?? this.successSoft,
      warning: warning ?? this.warning,
      warningSoft: warningSoft ?? this.warningSoft,
      danger: danger ?? this.danger,
      dangerSoft: dangerSoft ?? this.dangerSoft,
    );
  }

  @override
  AppPalette lerp(ThemeExtension<AppPalette>? other, double t) {
    if (other is! AppPalette) return this;
    Color l(Color a, Color b) => Color.lerp(a, b, t)!;
    return AppPalette(
      canvas: l(canvas, other.canvas),
      sidebar: l(sidebar, other.sidebar),
      surface: l(surface, other.surface),
      surfaceMuted: l(surfaceMuted, other.surfaceMuted),
      border: l(border, other.border),
      borderStrong: l(borderStrong, other.borderStrong),
      textPrimary: l(textPrimary, other.textPrimary),
      textSecondary: l(textSecondary, other.textSecondary),
      textMuted: l(textMuted, other.textMuted),
      accent: l(accent, other.accent),
      accentSoft: l(accentSoft, other.accentSoft),
      accentOnSoft: l(accentOnSoft, other.accentOnSoft),
      success: l(success, other.success),
      successSoft: l(successSoft, other.successSoft),
      warning: l(warning, other.warning),
      warningSoft: l(warningSoft, other.warningSoft),
      danger: l(danger, other.danger),
      dangerSoft: l(dangerSoft, other.dangerSoft),
    );
  }
}

extension AppPaletteContext on BuildContext {
  /// Paleta del tema activo. Cae a la clara si una prueba monta un
  /// `MaterialApp` sin el tema de la app (los widget tests lo hacen).
  AppPalette get palette {
    final theme = Theme.of(this);
    return theme.extension<AppPalette>() ??
        (theme.brightness == Brightness.dark
            ? AppPalette.dark
            : AppPalette.light);
  }
}
