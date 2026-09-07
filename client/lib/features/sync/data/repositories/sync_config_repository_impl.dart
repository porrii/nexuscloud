import 'package:shared_preferences/shared_preferences.dart';

import '../../domain/entities/auto_sync_settings.dart';
import '../../domain/entities/sync_pair.dart';
import '../../domain/repositories/sync_config_repository.dart';

/// Calcado de `LocalServerConfigStore` (core/storage) -- strings/primitivos
/// sueltos en `shared_preferences`, nada más.
class SyncConfigRepositoryImpl implements SyncConfigRepository {
  static const _remoteKey = 'nexuscloud.sync.remote_path';
  static const _localKey = 'nexuscloud.sync.local_path';
  static const _autoEnabledKey = 'nexuscloud.sync.auto_enabled';
  static const _autoIntervalKey = 'nexuscloud.sync.auto_interval_minutes';
  static const _autoLastAtKey = 'nexuscloud.sync.auto_last_at';
  static const _autoLastSummaryKey = 'nexuscloud.sync.auto_last_summary';

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

  @override
  Future<void> saveAutoSync(AutoSyncSettings settings) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool(_autoEnabledKey, settings.enabled);
    await prefs.setInt(_autoIntervalKey, settings.intervalMinutes);
  }

  @override
  Future<AutoSyncSettings> readAutoSync() async {
    final prefs = await SharedPreferences.getInstance();
    final enabled = prefs.getBool(_autoEnabledKey);
    final intervalMinutes = prefs.getInt(_autoIntervalKey);
    if (enabled == null || intervalMinutes == null) {
      return AutoSyncSettings.disabled;
    }
    return AutoSyncSettings(enabled: enabled, intervalMinutes: intervalMinutes);
  }

  @override
  Future<void> saveLastAutoSyncOutcome({
    required DateTime at,
    required String summary,
  }) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_autoLastAtKey, at.toIso8601String());
    await prefs.setString(_autoLastSummaryKey, summary);
  }

  @override
  Future<({DateTime at, String summary})?> readLastAutoSyncOutcome() async {
    final prefs = await SharedPreferences.getInstance();
    final at = prefs.getString(_autoLastAtKey);
    final summary = prefs.getString(_autoLastSummaryKey);
    if (at == null || summary == null) return null;
    return (at: DateTime.parse(at), summary: summary);
  }
}
