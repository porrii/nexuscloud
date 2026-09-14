import 'package:flutter/material.dart';

/// Tema mínimo para este slice — un único punto de crecimiento cuando haga
/// falta más (branding propio de NexusCloud), sin acoplarlo a cada
/// pantalla individual.
class AppTheme {
  const AppTheme._();

  static ThemeData light() => ThemeData(
        useMaterial3: true,
        colorScheme: ColorScheme.fromSeed(seedColor: Colors.indigo),
      );

  static ThemeData dark() => ThemeData(
        useMaterial3: true,
        colorScheme: ColorScheme.fromSeed(
          seedColor: Colors.indigo,
          brightness: Brightness.dark,
        ),
      );
}
