import 'dart:async';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/network/api_exception.dart';
import 'package:nexuscloud_client/features/auth/domain/entities/app_user.dart';
import 'package:nexuscloud_client/features/auth/domain/entities/auto_login_outcome.dart';
import 'package:nexuscloud_client/features/auth/domain/entities/login_result.dart';
import 'package:nexuscloud_client/features/auth/domain/repositories/auth_repository.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_listing.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_entry.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_entry.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_version.dart';
import 'package:nexuscloud_client/features/files/domain/repositories/files_repository.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/auto_sync_settings.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/local_trash_entry.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/pair_sync_outcome.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_direction.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_pair.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_pair_config.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_state_entry.dart';
import 'package:nexuscloud_client/features/sync/domain/repositories/local_trash_store.dart';
import 'package:nexuscloud_client/features/sync/domain/repositories/sync_config_repository.dart';
import 'package:nexuscloud_client/features/sync/domain/repositories/sync_state_store.dart';
import 'package:nexuscloud_client/features/sync/domain/services/auto_sync_scheduler.dart';
import 'package:nexuscloud_client/features/sync/domain/services/multi_pair_sync_coordinator.dart';
import 'package:nexuscloud_client/features/sync/domain/services/sync_engine.dart';

/// A propósito NO es un `Timer` real (ni siquiera `Timer(Duration.zero, ...)`):
/// un temporizador real de duración cero puede llegar a dispararse solo en
/// cuanto el test hace cualquier `await`, y entonces `isActive` pasaría a
/// `false` sin que el código bajo prueba haya llamado nunca a `.cancel()` --
/// un falso positivo justo en la aserción que se quiere comprobar. `Timer`
/// es `abstract interface class`, pensado para implementarse fuera de su
/// librería -- este fake solo cambia de estado cuando alguien llama a
/// `.cancel()` de verdad.
class _FakeTimer implements Timer {
  bool cancelled = false;

  @override
  void cancel() => cancelled = true;

  @override
  bool get isActive => !cancelled;

  @override
  int get tick => 0;
}

/// Captura la duración y el callback con los que `AutoSyncScheduler` arma
/// cada temporizador, y deja disparar ese callback a mano de forma
/// determinista -- sin esto haría falta un `Timer` real o el paquete
/// `fake_async` (ninguno de los dos es viable aquí, ver la nota de
/// `_FakeTimer`).
class _FakeTimerFactory {
  int createCount = 0;
  Duration? lastDuration;
  void Function(Timer timer)? _lastCallback;
  _FakeTimer? lastTimer;

  Timer call(Duration duration, void Function(Timer timer) callback) {
    createCount++;
    lastDuration = duration;
    _lastCallback = callback;
    final timer = _FakeTimer();
    lastTimer = timer;
    return timer;
  }

  /// Dispara el último callback capturado, como si el intervalo real
  /// hubiera transcurrido. El callback (`AutoSyncScheduler._tick`) es en
  /// realidad `async`, pero aquí se ve como `void Function(Timer)` (la
  /// firma real de `Timer.periodic`) -- quien llama a `fire()` debe
  /// esperar con `pumpEventQueue()` a que su trabajo interno termine.
  void fire() {
    final callback = _lastCallback;
    if (callback == null) {
      throw StateError('Ningún temporizador armado todavía.');
    }
    callback(lastTimer!);
  }
}

class _FakeAuthRepository implements AuthRepository {
  @override
  AppUser? currentUser;

  @override
  Stream<AppUser?> get userStream => throw UnimplementedError();

  @override
  Future<AutoLoginOutcome> tryAutoLogin() => throw UnimplementedError();

  @override
  Future<LoginResult> login({
    required String serverBaseUrl,
    required String username,
    required String password,
    String? totpCode,
  }) =>
      throw UnimplementedError();

  @override
  Future<void> logout() => throw UnimplementedError();
}

class _FakeSyncConfigRepository implements SyncConfigRepository {
  List<SyncPairConfig> pairs = [];
  AutoSyncSettings autoSync = AutoSyncSettings.disabled;
  ({DateTime at, String summary})? lastOutcome;
  int readCallCount = 0;

  @override
  Future<void> savePairs(List<SyncPairConfig> pairs) async => this.pairs = pairs;

  @override
  Future<List<SyncPairConfig>> readPairs() async {
    readCallCount++;
    return pairs;
  }

  @override
  Future<void> saveAutoSync(AutoSyncSettings settings) async => autoSync = settings;

  @override
  Future<AutoSyncSettings> readAutoSync() async => autoSync;

  @override
  Future<void> saveLastAutoSyncOutcome({
    required DateTime at,
    required String summary,
  }) async {
    lastOutcome = (at: at, summary: summary);
  }

  @override
  Future<({DateTime at, String summary})?> readLastAutoSyncOutcome() async =>
      lastOutcome;

  bool watchLocalChanges = false;

  @override
  Future<void> saveWatchLocalChanges(bool enabled) async =>
      watchLocalChanges = enabled;

  @override
  Future<bool> readWatchLocalChanges() async => watchLocalChanges;

  int maxAutoDeleteBatch = 10;

  @override
  Future<void> saveMaxAutoDeleteBatch(int value) async =>
      maxAutoDeleteBatch = value;

  @override
  Future<int> readMaxAutoDeleteBatch() async => maxAutoDeleteBatch;
}

/// Solo implementa `list` -- las únicas rutas de este archivo son
/// "listado vacío" (sincroniza sin descargar nada) y "ruta inexistente"
/// (fallo de arranque); ningún escenario llega a descargar un archivo de
/// verdad.
class _FakeFilesRepository implements FilesRepository {
  final Map<String, DirectoryListing> listingsByPath = {};

  @override
  Future<DirectoryListing> list(String path) async {
    final listing = listingsByPath[path];
    if (listing == null) {
      throw const ApiException(code: 'not_found', message: 'Carpeta no encontrada.');
    }
    return listing;
  }

  @override
  Future<FileEntry> uploadFile({
    required String parentPath,
    required String localFilePath,
    required String fileName,
    TransferProgress? onProgress,
  }) =>
      throw UnimplementedError();

  @override
  Future<void> createDirectory({
    required String parentPath,
    required String name,
  }) =>
      throw UnimplementedError();

  @override
  Future<void> downloadFile({
    required FileEntry file,
    required String saveToPath,
    TransferProgress? onProgress,
  }) =>
      throw UnimplementedError();

  @override
  Future<void> deleteFile(String fileId, {bool permanent = false}) =>
      throw UnimplementedError();

  @override
  Future<void> deleteDirectory(String directoryId, {bool permanent = false}) =>
      throw UnimplementedError();

  @override
  Future<FileEntry> moveFile(String fileId, {String? newParentPath, String? newName}) =>
      throw UnimplementedError();

  @override
  Future<DirectoryEntry> moveDirectory(String directoryId, {String? newParentPath, String? newName}) =>
      throw UnimplementedError();

  @override
  Future<void> restoreFile(String fileId) => throw UnimplementedError();

  @override
  Future<void> restoreDirectory(String directoryId) => throw UnimplementedError();

  @override
  Future<DirectoryListing> listTrash() => throw UnimplementedError();

  @override
  Future<List<FileVersion>> listVersions(String fileId) =>
      throw UnimplementedError();

  @override
  Future<void> downloadVersion({
    required String fileId,
    required FileVersion version,
    required String saveToPath,
    TransferProgress? onProgress,
  }) =>
      throw UnimplementedError();

  @override
  Future<FileEntry> restoreVersion({
    required String fileId,
    required int versionNum,
  }) =>
      throw UnimplementedError();
}

/// El manifiesto de estado no aporta nada a los escenarios de este archivo
/// (listado vacío / ruta inexistente), pero `SyncEngine` lo exige -- un
/// almacén en memoria basta.
class _InMemorySyncStateStore implements SyncStateStore {
  final Map<String, Map<String, SyncStateEntry>> _byPair = {};

  String _key(SyncPair pair) => '${pair.remotePath}|${pair.localPath}';

  @override
  Future<Map<String, SyncStateEntry>> read(SyncPair pair) async =>
      Map.of(_byPair[_key(pair)] ?? const {});

  @override
  Future<void> write(SyncPair pair, Map<String, SyncStateEntry> entries) async =>
      _byPair[_key(pair)] = Map.of(entries);
}

/// Ningún escenario de este archivo llega a borrar nada (listado vacío /
/// ruta inexistente), pero `SyncEngine` exige un `LocalTrashStore` -- un
/// fake que nunca debería llamarse basta.
class _InMemoryLocalTrashStore implements LocalTrashStore {
  @override
  Future<void> moveToTrash({
    required SyncPair pair,
    required List<String> relativeSegments,
    required File file,
  }) =>
      throw UnimplementedError('este archivo no ejercita borrados');

  @override
  Future<List<LocalTrashEntry>> listAll() =>
      throw UnimplementedError('este archivo no ejercita la papelera local');

  @override
  Future<void> restore({
    required LocalTrashEntry entry,
    required String destinationLocalPath,
  }) =>
      throw UnimplementedError('este archivo no ejercita la papelera local');

  @override
  Future<void> deleteForever(LocalTrashEntry entry) =>
      throw UnimplementedError('este archivo no ejercita la papelera local');
}

const _someUser = AppUser(
  id: 'u-1',
  username: 'ivan',
  displayName: 'Iván',
  hasTotp: false,
);

void main() {
  late _FakeTimerFactory timerFactory;
  late _FakeAuthRepository fakeAuth;
  late _FakeSyncConfigRepository fakeConfigRepo;
  late _FakeFilesRepository fakeFilesRepository;
  late SyncEngine syncEngine;
  late AutoSyncScheduler scheduler;

  setUp(() {
    timerFactory = _FakeTimerFactory();
    fakeAuth = _FakeAuthRepository();
    fakeConfigRepo = _FakeSyncConfigRepository();
    fakeFilesRepository = _FakeFilesRepository();
    syncEngine = SyncEngine(
      filesRepository: fakeFilesRepository,
      stateStore: _InMemorySyncStateStore(),
      trashStore: _InMemoryLocalTrashStore(),
    );
    scheduler = AutoSyncScheduler(
      coordinator: MultiPairSyncCoordinator(syncEngine: syncEngine),
      configRepository: fakeConfigRepo,
      authRepository: fakeAuth,
      createTimer: timerFactory.call,
    );
  });

  test('start arma un temporizador con la duración configurada si autoSync está activado', () async {
    fakeConfigRepo.autoSync = const AutoSyncSettings(enabled: true, intervalMinutes: 30);

    await scheduler.start();

    expect(timerFactory.createCount, 1);
    expect(timerFactory.lastDuration, const Duration(minutes: 30));
  });

  test('start no arma ningún temporizador si autoSync está desactivado', () async {
    // fakeConfigRepo.autoSync ya es AutoSyncSettings.disabled por defecto.
    await scheduler.start();

    expect(timerFactory.createCount, 0);
  });

  test('updateSettings con enabled:false cancela el temporizador existente', () async {
    await scheduler.updateSettings(
      const AutoSyncSettings(enabled: true, intervalMinutes: 15),
    );
    final armedTimer = timerFactory.lastTimer!;
    expect(armedTimer.cancelled, isFalse);

    await scheduler.updateSettings(AutoSyncSettings.disabled);

    expect(armedTimer.cancelled, isTrue);
    expect(fakeConfigRepo.autoSync, AutoSyncSettings.disabled);
  });

  test(
    'un tick sin sesión activa no hace nada, ni siquiera lee los pares configurados',
    () async {
      fakeAuth.currentUser = null;
      fakeConfigRepo.pairs = [
        const SyncPairConfig(
          pair: SyncPair(remotePath: '/x', localPath: '/y'),
          direction: SyncDirection.download,
        ),
      ];
      await scheduler.updateSettings(
        const AutoSyncSettings(enabled: true, intervalMinutes: 5),
      );

      timerFactory.fire();
      await pumpEventQueue();

      expect(fakeConfigRepo.readCallCount, 0);
      expect(fakeConfigRepo.lastOutcome, isNull);
    },
  );

  test(
    'un tick con sesión pero sin pares configurados no sincroniza nada',
    () async {
      fakeAuth.currentUser = _someUser;
      fakeConfigRepo.pairs = [];
      await scheduler.updateSettings(
        const AutoSyncSettings(enabled: true, intervalMinutes: 5),
      );

      timerFactory.fire();
      await pumpEventQueue();

      expect(fakeConfigRepo.readCallCount, 1);
      expect(fakeConfigRepo.lastOutcome, isNull);
      expect(syncEngine.isRunning, isFalse);
    },
  );

  test(
    'un tick con sesión y un par configurado sincroniza y persiste el resultado',
    () async {
      fakeAuth.currentUser = _someUser;
      fakeConfigRepo.pairs = [
        const SyncPairConfig(
          pair: SyncPair(remotePath: '/sync-root', localPath: '/no-se-toca-en-este-test'),
          direction: SyncDirection.download,
        ),
      ];
      fakeFilesRepository.listingsByPath['/sync-root'] =
          const DirectoryListing(directories: [], files: []);
      await scheduler.updateSettings(
        const AutoSyncSettings(enabled: true, intervalMinutes: 5),
      );

      final results = <List<PairSyncOutcome>>[];
      final subscription = scheduler.onResult.listen(results.add);

      timerFactory.fire();
      await pumpEventQueue();

      expect(results, hasLength(1));
      expect(results.single, hasLength(1));
      expect(results.single.single.result?.downloaded, 0);
      expect(fakeConfigRepo.lastOutcome, isNotNull);
      expect(fakeConfigRepo.lastOutcome!.summary, contains('0 descargados'));
      await subscription.cancel();
    },
  );

  test(
    'un tick sincroniza VARIOS pares configurados en la misma pasada',
    () async {
      fakeAuth.currentUser = _someUser;
      fakeConfigRepo.pairs = const [
        SyncPairConfig(
          pair: SyncPair(remotePath: '/root-a', localPath: '/local-a'),
          direction: SyncDirection.download,
        ),
        SyncPairConfig(
          pair: SyncPair(remotePath: '/root-b', localPath: '/local-b'),
          direction: SyncDirection.download,
        ),
      ];
      fakeFilesRepository.listingsByPath['/root-a'] =
          const DirectoryListing(directories: [], files: []);
      fakeFilesRepository.listingsByPath['/root-b'] =
          const DirectoryListing(directories: [], files: []);
      await scheduler.updateSettings(
        const AutoSyncSettings(enabled: true, intervalMinutes: 5),
      );

      final results = <List<PairSyncOutcome>>[];
      final subscription = scheduler.onResult.listen(results.add);

      timerFactory.fire();
      await pumpEventQueue();

      expect(results, hasLength(1));
      expect(results.single, hasLength(2));
      expect(results.single.map((o) => o.pair.remotePath), ['/root-a', '/root-b']);
      expect(fakeConfigRepo.lastOutcome!.summary, contains('local-a'));
      expect(fakeConfigRepo.lastOutcome!.summary, contains('local-b'));
      await subscription.cancel();
    },
  );

  test(
    'un tick salta los pares con autoSyncEnabled:false (#24)',
    () async {
      fakeAuth.currentUser = _someUser;
      fakeConfigRepo.pairs = const [
        SyncPairConfig(
          pair: SyncPair(remotePath: '/root-a', localPath: '/local-a'),
          direction: SyncDirection.download,
          autoSyncEnabled: true,
        ),
        SyncPairConfig(
          pair: SyncPair(remotePath: '/root-b', localPath: '/local-b'),
          direction: SyncDirection.download,
          autoSyncEnabled: false,
        ),
      ];
      // A propósito NO se registra ningún listado para '/root-b': si el
      // filtro por `autoSyncEnabled` no funcionara, el coordinador
      // intentaría sincronizarlo igual y `list()` lanzaría `not_found`,
      // apareciendo como un `startupError` en `results` -- este test
      // fallaría de forma clara en vez de solo "verse bien por casualidad".
      fakeFilesRepository.listingsByPath['/root-a'] =
          const DirectoryListing(directories: [], files: []);
      await scheduler.updateSettings(
        const AutoSyncSettings(enabled: true, intervalMinutes: 5),
      );

      final results = <List<PairSyncOutcome>>[];
      final subscription = scheduler.onResult.listen(results.add);

      timerFactory.fire();
      await pumpEventQueue();

      expect(results, hasLength(1));
      expect(results.single, hasLength(1));
      expect(results.single.single.pair.remotePath, '/root-a');
      await subscription.cancel();
    },
  );

  test(
    'un tick que falla en el arranque de un par persiste su error sin bloquear el resto',
    () async {
      fakeAuth.currentUser = _someUser;
      fakeConfigRepo.pairs = const [
        SyncPairConfig(
          pair: SyncPair(remotePath: '/no-existe', localPath: '/no-se-toca-en-este-test'),
          direction: SyncDirection.download,
        ),
        SyncPairConfig(
          pair: SyncPair(remotePath: '/root-b', localPath: '/local-b'),
          direction: SyncDirection.download,
        ),
      ];
      // No se registra ningún listado para '/no-existe' -> `list()` lanza.
      fakeFilesRepository.listingsByPath['/root-b'] =
          const DirectoryListing(directories: [], files: []);
      await scheduler.updateSettings(
        const AutoSyncSettings(enabled: true, intervalMinutes: 5),
      );

      final results = <List<PairSyncOutcome>>[];
      final subscription = scheduler.onResult.listen(results.add);

      timerFactory.fire();
      await pumpEventQueue();

      expect(results, hasLength(1));
      final outcomes = results.single;
      expect(outcomes[0].startupError, isNotNull);
      expect(outcomes[1].result?.downloaded, 0);
      expect(fakeConfigRepo.lastOutcome!.summary, contains('Error'));
      await subscription.cancel();
    },
  );
}
