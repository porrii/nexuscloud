import 'package:flutter/material.dart';

import 'app_palette.dart';

/// Texto monoespaciado (rutas, enlaces). En Windows `monospace` no es un
/// alias que el motor resuelva, así que se nombra una fuente del sistema.
const monoTextStyle = TextStyle(
  fontFamily: 'Consolas',
  fontFamilyFallback: ['Cascadia Mono', 'Courier New', 'monospace'],
);

/// Tema de la app -- mismo lenguaje visual que la web (grises pizarra,
/// azul como acento, bordes finos en vez de sombras), claro u oscuro
/// según el sistema. Todo el estilo de los componentes vive aquí para que
/// las pantallas no repitan colores ni radios a mano.
class AppTheme {
  const AppTheme._();

  static const radius = 8.0;
  static const radiusLarge = 12.0;

  static ThemeData light() => _build(AppPalette.light, Brightness.light);

  static ThemeData dark() => _build(AppPalette.dark, Brightness.dark);

  static ThemeData _build(AppPalette p, Brightness brightness) {
    final isDark = brightness == Brightness.dark;
    final scheme =
        ColorScheme.fromSeed(
          seedColor: Tw.blue600,
          brightness: brightness,
        ).copyWith(
          primary: p.accent,
          onPrimary: Colors.white,
          primaryContainer: p.accentSoft,
          onPrimaryContainer: p.accentOnSoft,
          secondary: p.textSecondary,
          surface: p.surface,
          onSurface: p.textPrimary,
          onSurfaceVariant: p.textSecondary,
          surfaceContainerLowest: p.surface,
          surfaceContainerLow: p.surface,
          surfaceContainer: p.surface,
          surfaceContainerHigh: p.surface,
          surfaceContainerHighest: p.surfaceMuted,
          outline: p.borderStrong,
          outlineVariant: p.border,
          error: p.danger,
          onError: Colors.white,
          errorContainer: p.dangerSoft,
          inverseSurface: isDark ? Tw.slate100 : Tw.slate900,
          onInverseSurface: isDark ? Tw.slate900 : Tw.slate50,
        );

    final base = ThemeData(
      useMaterial3: true,
      brightness: brightness,
      colorScheme: scheme,
      visualDensity: VisualDensity.standard,
      // App de escritorio con ratón: las áreas de pulsación se ajustan al
      // control en vez de inflarse a 48 px como en móvil.
      materialTapTargetSize: MaterialTapTargetSize.shrinkWrap,
    );

    final text = base.textTheme
        .apply(bodyColor: p.textPrimary, displayColor: p.textPrimary)
        .copyWith(
          headlineSmall: base.textTheme.headlineSmall?.copyWith(
            fontSize: 24,
            fontWeight: FontWeight.w600,
            color: p.textPrimary,
            letterSpacing: -0.2,
          ),
          titleLarge: base.textTheme.titleLarge?.copyWith(
            fontSize: 20,
            fontWeight: FontWeight.w600,
            color: p.textPrimary,
            letterSpacing: -0.1,
          ),
          titleMedium: base.textTheme.titleMedium?.copyWith(
            fontSize: 15,
            fontWeight: FontWeight.w600,
            color: p.textPrimary,
          ),
          titleSmall: base.textTheme.titleSmall?.copyWith(
            fontSize: 13.5,
            fontWeight: FontWeight.w600,
            color: p.textPrimary,
          ),
          bodyLarge: base.textTheme.bodyLarge?.copyWith(fontSize: 14.5),
          bodyMedium: base.textTheme.bodyMedium?.copyWith(fontSize: 14),
          bodySmall: base.textTheme.bodySmall?.copyWith(
            fontSize: 12.5,
            color: p.textMuted,
          ),
          labelLarge: base.textTheme.labelLarge?.copyWith(
            fontSize: 14,
            fontWeight: FontWeight.w500,
          ),
          labelMedium: base.textTheme.labelMedium?.copyWith(
            fontSize: 12.5,
            fontWeight: FontWeight.w500,
            color: p.textSecondary,
          ),
          labelSmall: base.textTheme.labelSmall?.copyWith(
            fontSize: 11.5,
            fontWeight: FontWeight.w600,
            color: p.textMuted,
            letterSpacing: 0.4,
          ),
        );

    final shape = RoundedRectangleBorder(
      borderRadius: BorderRadius.circular(radius),
    );
    const buttonPadding = EdgeInsets.symmetric(horizontal: 16, vertical: 12);
    const buttonMinSize = Size(64, 40);
    final buttonText = text.labelLarge;

    OutlineInputBorder inputBorder(Color color, [double width = 1]) =>
        OutlineInputBorder(
          borderRadius: BorderRadius.circular(radius),
          borderSide: BorderSide(color: color, width: width),
        );

    return base.copyWith(
      extensions: [p],
      scaffoldBackgroundColor: p.canvas,
      canvasColor: p.surface,
      dividerColor: p.border,
      textTheme: text,
      splashFactory: InkSparkle.splashFactory,
      hoverColor: p.surfaceMuted.withValues(alpha: isDark ? 0.6 : 0.9),
      focusColor: p.accent.withValues(alpha: 0.12),
      highlightColor: Colors.transparent,
      appBarTheme: AppBarTheme(
        backgroundColor: p.surface,
        foregroundColor: p.textPrimary,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        scrolledUnderElevation: 0,
        centerTitle: false,
        titleTextStyle: text.titleMedium,
        shape: Border(bottom: BorderSide(color: p.border)),
      ),
      dividerTheme: DividerThemeData(color: p.border, thickness: 1, space: 1),
      cardTheme: CardThemeData(
        color: p.surface,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        margin: EdgeInsets.zero,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(radiusLarge),
          side: BorderSide(color: p.border),
        ),
      ),
      dialogTheme: DialogThemeData(
        backgroundColor: p.surface,
        surfaceTintColor: Colors.transparent,
        elevation: 12,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(14),
          side: BorderSide(color: p.border),
        ),
        titleTextStyle: text.titleLarge?.copyWith(fontSize: 18),
        contentTextStyle: text.bodyMedium?.copyWith(color: p.textSecondary),
      ),
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          backgroundColor: p.accent,
          foregroundColor: Colors.white,
          disabledBackgroundColor: p.surfaceMuted,
          disabledForegroundColor: p.textMuted,
          minimumSize: buttonMinSize,
          padding: buttonPadding,
          shape: shape,
          textStyle: buttonText,
        ),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          foregroundColor: p.textPrimary,
          backgroundColor: p.surface,
          minimumSize: buttonMinSize,
          padding: buttonPadding,
          shape: shape,
          side: BorderSide(color: p.borderStrong),
          textStyle: buttonText,
        ),
      ),
      textButtonTheme: TextButtonThemeData(
        style: TextButton.styleFrom(
          foregroundColor: p.accent,
          minimumSize: const Size(48, 36),
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
          shape: shape,
          textStyle: buttonText,
        ),
      ),
      elevatedButtonTheme: ElevatedButtonThemeData(
        style: ElevatedButton.styleFrom(
          elevation: 0,
          backgroundColor: p.accent,
          foregroundColor: Colors.white,
          minimumSize: buttonMinSize,
          padding: buttonPadding,
          shape: shape,
          textStyle: buttonText,
        ),
      ),
      iconButtonTheme: IconButtonThemeData(
        style: IconButton.styleFrom(
          foregroundColor: p.textSecondary,
          shape: shape,
          iconSize: 20,
          minimumSize: const Size(36, 36),
          padding: const EdgeInsets.all(8),
        ),
      ),
      iconTheme: IconThemeData(color: p.textSecondary, size: 20),
      inputDecorationTheme: InputDecorationThemeData(
        filled: true,
        fillColor: p.surface,
        isDense: true,
        contentPadding: const EdgeInsets.symmetric(
          horizontal: 12,
          vertical: 14,
        ),
        border: inputBorder(p.borderStrong),
        enabledBorder: inputBorder(p.borderStrong),
        focusedBorder: inputBorder(p.accent, 1.6),
        errorBorder: inputBorder(p.danger),
        focusedErrorBorder: inputBorder(p.danger, 1.6),
        labelStyle: TextStyle(color: p.textSecondary),
        floatingLabelStyle: TextStyle(color: p.accent),
        hintStyle: TextStyle(color: p.textMuted),
        prefixIconColor: p.textMuted,
        suffixIconColor: p.textMuted,
      ),
      tooltipTheme: TooltipThemeData(
        waitDuration: const Duration(milliseconds: 450),
        textStyle: const TextStyle(color: Colors.white, fontSize: 12),
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 5),
        decoration: BoxDecoration(
          color: isDark ? Tw.slate700 : Tw.slate800,
          borderRadius: BorderRadius.circular(6),
        ),
      ),
      snackBarTheme: SnackBarThemeData(
        behavior: SnackBarBehavior.floating,
        backgroundColor: isDark ? Tw.slate100 : Tw.slate900,
        contentTextStyle: TextStyle(
          color: isDark ? Tw.slate900 : Colors.white,
          fontSize: 14,
        ),
        actionTextColor: isDark ? Tw.blue600 : Tw.blue300,
        width: 480,
        shape: shape,
      ),
      popupMenuTheme: PopupMenuThemeData(
        color: p.surface,
        surfaceTintColor: Colors.transparent,
        elevation: 8,
        shadowColor: Colors.black.withValues(alpha: isDark ? 0.5 : 0.18),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(10),
          side: BorderSide(color: p.border),
        ),
        textStyle: text.bodyMedium,
        menuPadding: const EdgeInsets.symmetric(vertical: 6),
      ),
      menuTheme: MenuThemeData(
        style: MenuStyle(
          backgroundColor: WidgetStatePropertyAll(p.surface),
          surfaceTintColor: const WidgetStatePropertyAll(Colors.transparent),
          elevation: const WidgetStatePropertyAll(8),
          shadowColor: WidgetStatePropertyAll(
            Colors.black.withValues(alpha: isDark ? 0.5 : 0.18),
          ),
          shape: WidgetStatePropertyAll(
            RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(10),
              side: BorderSide(color: p.border),
            ),
          ),
        ),
      ),
      listTileTheme: ListTileThemeData(
        shape: shape,
        iconColor: p.textSecondary,
        subtitleTextStyle: text.bodySmall,
      ),
      switchTheme: SwitchThemeData(
        thumbColor: WidgetStateProperty.resolveWith(
          (states) => states.contains(WidgetState.selected)
              ? Colors.white
              : (isDark ? Tw.slate400 : Colors.white),
        ),
        trackColor: WidgetStateProperty.resolveWith(
          (states) => states.contains(WidgetState.selected)
              ? p.accent
              : (isDark ? Tw.slate700 : Tw.slate300),
        ),
        trackOutlineColor: const WidgetStatePropertyAll(Colors.transparent),
        thumbIcon: const WidgetStatePropertyAll(null),
      ),
      checkboxTheme: CheckboxThemeData(
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(4)),
        side: BorderSide(color: p.borderStrong, width: 1.5),
      ),
      segmentedButtonTheme: SegmentedButtonThemeData(
        style: SegmentedButton.styleFrom(
          backgroundColor: p.surface,
          foregroundColor: p.textSecondary,
          selectedBackgroundColor: p.accentSoft,
          selectedForegroundColor: p.accentOnSoft,
          side: BorderSide(color: p.borderStrong),
          shape: shape,
          textStyle: text.labelMedium,
        ),
      ),
      progressIndicatorTheme: ProgressIndicatorThemeData(
        color: p.accent,
        linearTrackColor: p.surfaceMuted,
        circularTrackColor: Colors.transparent,
        linearMinHeight: 4,
        borderRadius: BorderRadius.circular(4),
      ),
      scrollbarTheme: ScrollbarThemeData(
        thickness: const WidgetStatePropertyAll(6),
        radius: const Radius.circular(6),
        thumbColor: WidgetStatePropertyAll(
          p.borderStrong.withValues(alpha: 0.9),
        ),
      ),
      tabBarTheme: TabBarThemeData(
        labelColor: p.accent,
        unselectedLabelColor: p.textSecondary,
        indicatorColor: p.accent,
        indicatorSize: TabBarIndicatorSize.label,
        dividerColor: p.border,
        labelStyle: text.labelLarge?.copyWith(fontWeight: FontWeight.w600),
        unselectedLabelStyle: text.labelLarge,
        overlayColor: WidgetStatePropertyAll(p.surfaceMuted),
        tabAlignment: TabAlignment.start,
      ),
      chipTheme: ChipThemeData(
        color: WidgetStateProperty.resolveWith(
          (states) =>
              states.contains(WidgetState.selected) ? p.accentSoft : p.surface,
        ),
        side: WidgetStateBorderSide.resolveWith(
          (states) => BorderSide(
            color: states.contains(WidgetState.selected)
                ? p.accent.withValues(alpha: 0.5)
                : p.borderStrong,
          ),
        ),
        labelStyle: text.labelMedium?.copyWith(color: p.textSecondary),
        secondaryLabelStyle: text.labelMedium?.copyWith(
          color: p.accentOnSoft,
          fontWeight: FontWeight.w600,
        ),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(999)),
        padding: const EdgeInsets.symmetric(horizontal: 4),
      ),
      datePickerTheme: DatePickerThemeData(
        backgroundColor: p.surface,
        surfaceTintColor: Colors.transparent,
      ),
      timePickerTheme: TimePickerThemeData(backgroundColor: p.surface),
      dropdownMenuTheme: DropdownMenuThemeData(
        menuStyle: MenuStyle(
          backgroundColor: WidgetStatePropertyAll(p.surface),
          surfaceTintColor: const WidgetStatePropertyAll(Colors.transparent),
        ),
      ),
    );
  }
}
