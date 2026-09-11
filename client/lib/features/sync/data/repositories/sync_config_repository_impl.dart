import 'dart:convert';

import 'package:shared_preferences/shared_preferences.dart';

import '../../domain/entities/auto_sync_settings.dart';
import '../../domain/entities/sync_direction.dart';
import '../../domain/entities/sync_pair.dart';
import '../../domain/entities/sync_pair_config.dart';
import '../../domain/repositories/sync_config_repository.dart';

/// Calcado de `LocalServerConfigStore` (core/storage) -- strings/primitivos
/// sueltos en `shared_preferences`, salvo la lista de pares, que sí necesita
/// JSON (`SyncPairConfig.toJson`/`fromJson`, ADR-014).
class SyncConfigRepositoryImpl implements SyncConfigRepository {
  static const _pairsKey = 'nexuscloud.sync.pairs';
  static const _autoEnabledKey = 'nexuscloud.sync.auto_enabled';
  static const _autoIntervalKey = 'nexuscloud.sync.auto_interval_minutes';
  static const _autoLastAtKey = 'nexuscloud.sync.auto_last_at';
  static const _autoLastSummaryKey = 'nexuscloud.sync.auto_last_summary';

  // Claves de antes del slice 15 (un único par + una única dirección
  // globales) -- solo se leen para migrar, nunca se vuelven a escribir.
  static const _legacyRemoteKey = 'nexuscloud.sync.remote_path';
  static const _legacyLocalKey = 'nexuscloud.sync.local_path';
  static const _legacyDirectionKey = 'nexuscloud.sync.direction';

  @override
  Future<void> savePairs(List<SyncPairConfig> pairs) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(
      _pairsKey,
      jsonEncode([for (final p in pairs) p.toJson()]),
    );
  }

  @override
  Future<List<SyncPairConfig>> readPairs() async {
    final prefs = await SharedPreferences.getInstance();
    final raw = prefs.getString(_pairsKey);
    if (raw != null) {
      return _decodePairs(raw);
    }
    return _migrateLegacyPair(prefs);
  }

  List<SyncPairConfig> _decodePairs(String raw) {
    final decoded = jsonDecode(raw);
    if (decoded is! List) return [];
    return [
      for (final entry in decoded)
        if (entry is Map)
          SyncPairConfig.fromJson(Map<String, dynamic>.from(entry)),
    ];
  }

  /// Migración transparente desde el único par que guardaban los slices A-14
  /// (`remote_path`/`local_path`, con `direction` opcional -- ese ajuste no
  /// existía antes del slice 13). Se ejecuta como mucho una vez: en cuanto
  /// encuentra algo que migrar, lo persiste ya bajo [_pairsKey] y borra las
  /// claves viejas, así una segunda llamada entra directa por la rama
  /// `raw != null` de [readPairs] sin volver a mirar aquí.
  Future<List<SyncPairConfig>> _migrateLegacyPair(SharedPreferences prefs) async {
    final remote = prefs.getString(_legacyRemoteKey);
    final local = prefs.getString(_legacyLocalKey);
    if (remote == null || local == null) return [];

    final migrated = [
      SyncPairConfig(
        pair: SyncPair(remotePath: remote, localPath: local),
        direction: SyncDirection.fromStorage(prefs.getString(_legacyDirectionKey)),
      ),
    ];
    await savePairs(migrated);
    await prefs.remove(_legacyRemoteKey);
    await prefs.remove(_legacyLocalKey);
    await prefs.remove(_legacyDirectionKey);
    return migrated;
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
