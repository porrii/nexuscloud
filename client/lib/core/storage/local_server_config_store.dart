import 'package:shared_preferences/shared_preferences.dart';

import 'server_config_store.dart';

class LocalServerConfigStore implements ServerConfigStore {
  static const _key = 'nexuscloud.server_base_url';

  @override
  Future<void> save(String baseUrl) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_key, baseUrl);
  }

  @override
  Future<String?> read() async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getString(_key);
  }

  @override
  Future<void> clear() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.remove(_key);
  }
}
