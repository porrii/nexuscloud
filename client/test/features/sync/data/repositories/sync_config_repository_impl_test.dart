import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/features/sync/data/repositories/sync_config_repository_impl.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/auto_sync_settings.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_direction.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_pair.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_pair_config.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  test('readPairs devuelve una lista vacía si no hay nada guardado', () async {
    final repo = SyncConfigRepositoryImpl();

    expect(await repo.readPairs(), isEmpty);
  });

  test('savePairs y readPairs hacen round-trip con varios pares y direcciones', () async {
    final repo = SyncConfigRepositoryImpl();
    const pairs = [
      SyncPairConfig(
        pair: SyncPair(remotePath: '/Documentos', localPath: r'C:\Users\ivan\Documentos'),
        direction: SyncDirection.both,
      ),
      SyncPairConfig(
        pair: SyncPair(remotePath: '/Fotos', localPath: r'C:\Users\ivan\Fotos'),
        direction: SyncDirection.upload,
      ),
    ];

    await repo.savePairs(pairs);

    expect(await repo.readPairs(), pairs);
  });

  test('savePairs con una lista vacía deja readPairs en vacío', () async {
    final repo = SyncConfigRepositoryImpl();
    await repo.savePairs(const [
      SyncPairConfig(
        pair: SyncPair(remotePath: '/', localPath: '/tmp'),
        direction: SyncDirection.download,
      ),
    ]);

    await repo.savePairs(const []);

    expect(await repo.readPairs(), isEmpty);
  });

  group('migración desde el único par de antes del slice 15', () {
    test('con remote_path/local_path/direction antiguos, migra a una lista de un elemento', () async {
      SharedPreferences.setMockInitialValues({
        'nexuscloud.sync.remote_path': '/Documentos',
        'nexuscloud.sync.local_path': r'C:\local',
        'nexuscloud.sync.direction': 'both',
      });
      final repo = SyncConfigRepositoryImpl();

      final pairs = await repo.readPairs();

      expect(pairs, [
        const SyncPairConfig(
          pair: SyncPair(remotePath: '/Documentos', localPath: r'C:\local'),
          direction: SyncDirection.both,
        ),
      ]);
    });

    test('sin direction antiguo (de antes del slice 13), usa download por defecto', () async {
      SharedPreferences.setMockInitialValues({
        'nexuscloud.sync.remote_path': '/',
        'nexuscloud.sync.local_path': r'C:\local',
      });
      final repo = SyncConfigRepositoryImpl();

      final pairs = await repo.readPairs();

      expect(pairs.single.direction, SyncDirection.download);
    });

    test('la migración se persiste y las claves antiguas desaparecen -- no se repite', () async {
      SharedPreferences.setMockInitialValues({
        'nexuscloud.sync.remote_path': '/',
        'nexuscloud.sync.local_path': r'C:\local',
      });
      final repo = SyncConfigRepositoryImpl();

      final first = await repo.readPairs();
      final prefs = await SharedPreferences.getInstance();
      expect(prefs.containsKey('nexuscloud.sync.remote_path'), isFalse);
      expect(prefs.containsKey('nexuscloud.sync.local_path'), isFalse);
      expect(prefs.containsKey('nexuscloud.sync.direction'), isFalse);

      // Segunda lectura: entra directa por la lista ya persistida, no por
      // la migración (que ya no tiene nada que mirar).
      final second = await repo.readPairs();
      expect(second, first);
    });
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

  test('readWatchLocalChanges devuelve false si no hay nada guardado', () async {
    final repo = SyncConfigRepositoryImpl();

    expect(await repo.readWatchLocalChanges(), isFalse);
  });

  test('saveWatchLocalChanges y readWatchLocalChanges hacen round-trip', () async {
    final repo = SyncConfigRepositoryImpl();

    await repo.saveWatchLocalChanges(true);
    expect(await repo.readWatchLocalChanges(), isTrue);

    await repo.saveWatchLocalChanges(false);
    expect(await repo.readWatchLocalChanges(), isFalse);
  });
}
