import 'package:shared_preferences/shared_preferences.dart';

import '../../domain/entities/sync_pair.dart';
import '../../domain/repositories/sync_config_repository.dart';

/// Calcado de `LocalServerConfigStore` (core/storage) -- dos strings en
/// `shared_preferences`, nada más.
class SyncConfigRepositoryImpl implements SyncConfigRepository {
  static const _remoteKey = 'nexuscloud.sync.remote_path';
  static const _localKey = 'nexuscloud.sync.local_path';

  @override
  Future<void> save(SyncPair pair) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_remoteKey, pair.remotePath);
    await prefs.setString(_localKey, pair.localPath);
  }

  @override
  Future<SyncPair?> read() async {
    final prefs = await SharedPreferences.getInstance();
    final remote = prefs.getString(_remoteKey);
    final local = prefs.getString(_localKey);
    if (remote == null || local == null) return null;
    return SyncPair(remotePath: remote, localPath: local);
  }

  @override
  Future<void> clear() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.remove(_remoteKey);
    await prefs.remove(_localKey);
  }
}
