import 'package:shared_preferences/shared_preferences.dart';

import 'window_preferences_store.dart';

class LocalWindowPreferencesStore implements WindowPreferencesStore {
  static const _minimizeToTrayOnCloseKey =
      'nexuscloud.window.minimize_to_tray_on_close';
  static const _startMinimizedKey = 'nexuscloud.window.start_minimized';

  @override
  Future<void> saveMinimizeToTrayOnClose(bool value) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool(_minimizeToTrayOnCloseKey, value);
  }

  @override
  Future<bool> readMinimizeToTrayOnClose() async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getBool(_minimizeToTrayOnCloseKey) ?? false;
  }

  @override
  Future<void> saveStartMinimized(bool value) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool(_startMinimizedKey, value);
  }

  @override
  Future<bool> readStartMinimized() async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getBool(_startMinimizedKey) ?? false;
  }
}
