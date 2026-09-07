import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/features/sync/data/repositories/sync_config_repository_impl.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/auto_sync_settings.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_pair.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  test('read devuelve null si no hay nada guardado todavía', () async {
    final repo = SyncConfigRepositoryImpl();

    expect(await repo.read(), isNull);
  });

  test('save y read devuelven el mismo par', () async {
    final repo = SyncConfigRepositoryImpl();
    const pair = SyncPair(
      remotePath: '/Documentos',
      localPath: r'C:\Users\ivan\NexusCloud',
    );

    await repo.save(pair);

    expect(await repo.read(), pair);
  });

  test('clear borra el par guardado', () async {
    final repo = SyncConfigRepositoryImpl();
    await repo.save(const SyncPair(remotePath: '/', localPath: '/tmp'));

    await repo.clear();

    expect(await repo.read(), isNull);
  });

  test('readAutoSync devuelve AutoSyncSettings.disabled si no hay nada guardado', () async {
    final repo = SyncConfigRepositoryImpl();

    expect(await repo.readAutoSync(), AutoSyncSettings.disabled);
  });

  test('saveAutoSync y readAutoSync devuelven los mismos ajustes', () async {
    final repo = SyncConfigRepositoryImpl();
    const settings = AutoSyncSettings(enabled: true, intervalMinutes: 30);

    await repo.saveAutoSync(settings);

    expect(await repo.readAutoSync(), settings);
  });

  test('readLastAutoSyncOutcome devuelve null si no hay nada guardado todavía', () async {
    final repo = SyncConfigRepositoryImpl();

    expect(await repo.readLastAutoSyncOutcome(), isNull);
  });

  test('saveLastAutoSyncOutcome y readLastAutoSyncOutcome devuelven lo mismo', () async {
    final repo = SyncConfigRepositoryImpl();
    final at = DateTime.utc(2026, 3, 1, 10, 30);

    await repo.saveLastAutoSyncOutcome(at: at, summary: '3 descargados, 0 errores');
    final outcome = await repo.readLastAutoSyncOutcome();

    expect(outcome, isNotNull);
    expect(outcome!.at, at);
    expect(outcome.summary, '3 descargados, 0 errores');
  });
}
