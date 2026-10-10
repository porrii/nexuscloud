import 'package:shared_preferences/shared_preferences.dart';

/// Preferencias de presentación del explorador que sobreviven a reiniciar
/// la app. No son secretas -- mismo criterio que `WindowPreferencesStore`.
class BrowserPreferences {
  static const _gridViewKey = 'nexuscloud.browser.grid_view';

  /// `false` (vista de lista) si no hay nada guardado todavía.
  Future<bool> readGridView() async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getBool(_gridViewKey) ?? false;
  }

  Future<void> saveGridView(bool value) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool(_gridViewKey, value);
  }
}
