import 'dart:async';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/features/auth/domain/entities/app_user.dart';
import 'package:nexuscloud_client/features/auth/domain/entities/auto_login_outcome.dart';
import 'package:nexuscloud_client/features/auth/domain/entities/login_result.dart';
import 'package:nexuscloud_client/features/auth/domain/repositories/auth_repository.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_listing.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_entry.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_version.dart';
import 'package:nexuscloud_client/features/files/domain/repositories/files_repository.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/auto_sync_settings.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/local_trash_entry.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_direction.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_pair.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_pair_config.dart';
import 'package:nexuscloud_client/features/sync/domain/entities/sync_state_entry.dart';
import 'package:nexuscloud_client/features/sync/domain/repositories/local_trash_store.dart';
import 'package:nexuscloud_client/features/sync/domain/repositories/sync_config_repository.dart';
import 'package:nexuscloud_client/features/sync/domain/repositories/sync_state_store.dart';
import 'package:nexuscloud_client/features/sync/domain/services/local_change_watcher_service.dart';
import 'package:nexuscloud_client/features/sync/domain/services/multi_pair_sync_coordinator.dart';
import 'package:nexuscloud_client/features/sync/domain/services/sync_engine.dart';
import 'package:watcher/watcher.dart';

/// A propósito NO es un `Timer` real -- ver la misma nota en
/// `auto_sync_scheduler_test.dart`: un temporizador real de duración cero
/// puede dispararse solo en cuanto el test hace cualquier `await`, dando un
/// falso positivo justo en la aserción sobre `.cancel()`.
class _FakeTimer implements Timer {
  _FakeTimer(this._callback);
  final void Function() _callback;
  bool cancelled = false;
  bool fired = false;

  @override
  void cancel() => cancelled = true;

  @override
  bool get isActive => !cancelled && !fired;

  @override
  int get tick => 0;

  void fire() {
    if (cancelled) return;
    fired = true;
    _callback();
  }
}

/// A diferencia de `_FakeTimerFactory` de `AutoSyncScheduler` (un temporizador
/// PERIÓDICO global), aquí el servicio bajo prueba arma un temporizador de UN
/// disparo POR PAR -- esta factory registra todos los que se van creando, en
/// orden, para poder disparar "el último" (el que gana el debounce si el
/// código bajo prueba canceló los anteriores correctamente, como se espera).
class _FakeDebounceTimerFactory {
  final created = <_FakeTimer>[];
  int get createCount => created.length;

  Timer call(Duration duration, void Function() callback) {
    final timer = _FakeTimer(callback);
    created.add(timer);
    return timer;
  }
}

/// Un `StreamController` por carpeta vigilada -- el test emite eventos/
/// errores sintéticos sin tocar el filesystem real ni depender de que un
/// `DirectoryWatcher` de verdad detecte nada.
class _FakeWatcherFactory {
  final controllers = <String, StreamController<WatchEvent>>{};
  int get watchedPathCount => controllers.length;

  Stream<WatchEvent> call(String path) {
    final controller = StreamController<WatchEvent>.broadcast();
    controllers[path] = controller;
    return controller.stream;
  }

  void emit(String path) {
    controllers[path]!.add(WatchEvent(ChangeType.MODIFY, path));
  }

  void emitError(String path) {
    controllers[path]!.addError(const FileSystemException('boom'));
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
  bool watchEnabled = false;

  @override
  Future<void> savePairs(List<SyncPairConfig> pairs) async => this.pairs = pairs;

  @override
  Future<List<SyncPairConfig>> readPairs() async => pairs;

  @override
  Future<void> saveAutoSync(AutoSyncSettings settings) async {}

  @override
  Future<AutoSyncSettings> readAutoSync() async => AutoSyncSettings.disabled;

  @override
  Future<void> saveLastAutoSyncOutcome({required DateTime at, required String summary}) async {}

  @override
  Future<({DateTime at, String summary})?> readLastAutoSyncOutcome() async => null;

  @override
  Future<void> saveWatchLocalChanges(bool enabled) async => watchEnabled = enabled;

  @override
  Future<bool> readWatchLocalChanges() async => watchEnabled;
}

/// Árbol remoto vacío por defecto para todos los pares -- ningún escenario
/// de este archivo necesita un contenido real, solo contar CUÁNTAS veces se
/// llegó a llamar `list()` (proxy de "se disparó una sincronización").
class _FakeFilesRepository implements FilesRepository {
  final Map<String, DirectoryListing> listingsByPath = {};
  int listCallCount = 0;

  /// Deja una llamada a `list()` colgada hasta que el test la complete --
  /// necesario para simular "ese par ya está sincronizándose" de forma
  /// determinista (sin esto, una sync de fondo sin nada que la retenga
  /// termina casi al instante, y `isPairRunning` ya no es `true` cuando el
  /// test llega a comprobarlo).
  Completer<void>? listGate;

  @override
  Future<DirectoryListing> list(String path) async {
    listCallCount++;
    final gate = listGate;
    if (gate != null) await gate.future;
    return listingsByPath[path] ?? const DirectoryListing(directories: [], files: []);
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
  Future<void> createDirectory({required String parentPath, required String name}) =>
      throw UnimplementedError();

  @override
  Future<void> downloadFile({
    required FileEntry file,
    required String saveToPath,
    TransferProgress? onProgress,
  }) =>
      throw UnimplementedError();

  @override
  Future<void> deleteFile(String fileId, {bool permanent = false}) => throw UnimplementedError();

  @override
  Future<void> deleteDirectory(String directoryId, {bool permanent = false}) =>
      throw UnimplementedError();

  @override
  Future<void> restoreFile(String fileId) => throw UnimplementedError();

  @override
  Future<void> restoreDirectory(String directoryId) => throw UnimplementedError();

  @override
  Future<DirectoryListing> listTrash() => throw UnimplementedError();

  @override
  Future<List<FileVersion>> listVersions(String fileId) => throw UnimplementedError();

  @override
  Future<void> downloadVersion({
    required String fileId,
    required FileVersion version,
    required String saveToPath,
    TransferProgress? onProgress,
  }) =>
      throw UnimplementedError();

  @override
  Future<FileEntry> restoreVersion({required String fileId, required int versionNum}) =>
      throw UnimplementedError();
}

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

class _InMemoryLocalTrashStore implements LocalTrashStore {
  @override
  Future<void> moveToTrash({
    required SyncPair pair,
    required List<String> relativeSegments,
    required File file,
  }) =>
      throw UnimplementedError('este archivo no ejercita borrados');

  @override
  Future<List<LocalTrashEntry>> listAll() async => [];

  @override
  Future<void> restore({required LocalTrashEntry entry, required String destinationLocalPath}) =>
      throw UnimplementedError('este archivo no ejercita la papelera local');

  @override
  Future<void> deleteForever(LocalTrashEntry entry) =>
      throw UnimplementedError('este archivo no ejercita la papelera local');
}

const _someUser = AppUser(id: 'u-1', username: 'ivan', displayName: 'Iván', hasTotp: false);

void main() {
  late _FakeAuthRepository fakeAuth;
  late _FakeSyncConfigRepository fakeConfigRepo;
  late _FakeFilesRepository fakeFilesRepository;
  late _FakeWatcherFactory watcherFactory;
  late _FakeDebounceTimerFactory timerFactory;
  late SyncEngine syncEngine;
  late MultiPairSyncCoordinator coordinator;
  late LocalChangeWatcherService service;

  const pairAmbos = SyncPairConfig(
    pair: SyncPair(remotePath: '/root-a', localPath: '/local-a'),
    direction: SyncDirection.both,
  );
  const pairDescargar = SyncPairConfig(
    pair: SyncPair(remotePath: '/root-b', localPath: '/local-b'),
    direction: SyncDirection.download,
  );

  setUp(() {
    fakeAuth = _FakeAuthRepository()..currentUser = _someUser;
    fakeConfigRepo = _FakeSyncConfigRepository();
    fakeFilesRepository = _FakeFilesRepository();
    watcherFactory = _FakeWatcherFactory();
    timerFactory = _FakeDebounceTimerFactory();
    syncEngine = SyncEngine(
      filesRepository: fakeFilesRepository,
      stateStore: _InMemorySyncStateStore(),
      trashStore: _InMemoryLocalTrashStore(),
    );
    coordinator = MultiPairSyncCoordinator(syncEngine: syncEngine);
    service = LocalChangeWatcherService(
      coordinator: coordinator,
      syncEngine: syncEngine,
      configRepository: fakeConfigRepo,
      authRepository: fakeAuth,
      watchDirectory: watcherFactory.call,
      createDebounceTimer: timerFactory.call,
    );
  });

  test('start() no arma ningún watcher si el ajuste está desactivado', () async {
    fakeConfigRepo.pairs = [pairAmbos];
    // watchEnabled ya es false por defecto.

    await service.start();

    expect(watcherFactory.watchedPathCount, 0);
  });

  test('un par en Ambos dispara syncAllNow tras completarse el debounce, no antes', () async {
    fakeConfigRepo.watchEnabled = true;
    fakeConfigRepo.pairs = [pairAmbos];
    await service.start();

    watcherFactory.emit(pairAmbos.pair.localPath);
    await pumpEventQueue();
    expect(fakeFilesRepository.listCallCount, 0, reason: 'todavía no venció el debounce');

    timerFactory.created.single.fire();
    await pumpEventQueue();

    expect(fakeFilesRepository.listCallCount, 1);
  });

  test(
    'varios eventos seguidos antes de que venza el debounce reinician el temporizador -- solo UNA sync',
    () async {
      fakeConfigRepo.watchEnabled = true;
      fakeConfigRepo.pairs = [pairAmbos];
      await service.start();

      watcherFactory.emit(pairAmbos.pair.localPath);
      watcherFactory.emit(pairAmbos.pair.localPath);
      watcherFactory.emit(pairAmbos.pair.localPath);
      await pumpEventQueue();

      expect(timerFactory.createCount, 3, reason: 'cada evento arma un temporizador nuevo');
      expect(timerFactory.created[0].cancelled, isTrue, reason: 'reiniciado por el 2º evento');
      expect(timerFactory.created[1].cancelled, isTrue, reason: 'reiniciado por el 3º evento');

      timerFactory.created.last.fire();
      await pumpEventQueue();

      expect(fakeFilesRepository.listCallCount, 1);
    },
  );

  test('un par en Descargar nunca arma un watcher', () async {
    fakeConfigRepo.watchEnabled = true;
    fakeConfigRepo.pairs = [pairDescargar];

    await service.start();

    expect(watcherFactory.watchedPathCount, 0);
  });

  test(
    'si el par ya está sincronizándose cuando vence el debounce, no se apila otra sync',
    () async {
      fakeConfigRepo.watchEnabled = true;
      fakeConfigRepo.pairs = [pairAmbos];
      await service.start();

      // Arranca una sync "manual" para ese mismo par y la deja colgada a
      // propósito (gate sin completar) -- simula que ya está en curso.
      fakeFilesRepository.listGate = Completer<void>();
      unawaited(syncEngine.syncNow(pairAmbos.pair, direction: pairAmbos.direction));
      await pumpEventQueue();
      expect(syncEngine.isPairRunning(pairAmbos.pair), isTrue);
      final callsBeforeEvent = fakeFilesRepository.listCallCount;
      expect(callsBeforeEvent, 1, reason: 'la sync de fondo ya entró a list(), colgada en el gate');

      watcherFactory.emit(pairAmbos.pair.localPath);
      await pumpEventQueue();
      timerFactory.created.single.fire();
      await pumpEventQueue();

      // Ni una llamada nueva a list() -- el guard evitó disparar syncAllNow
      // mientras la sync de fondo sigue en curso.
      expect(fakeFilesRepository.listCallCount, callsBeforeEvent);

      fakeFilesRepository.listGate!.complete();
    },
  );

  test('updatePairs: quitar un par cancela su watcher (deja de reaccionar)', () async {
    fakeConfigRepo.watchEnabled = true;
    fakeConfigRepo.pairs = [pairAmbos];
    await service.start();
    expect(watcherFactory.watchedPathCount, 1);

    await service.updatePairs([]);

    watcherFactory.emit(pairAmbos.pair.localPath);
    await pumpEventQueue();

    expect(timerFactory.createCount, 0, reason: 'el watcher ya no debería estar escuchando');
  });

  test('updatePairs: añadir un par nuevo empieza a reaccionar a sus eventos', () async {
    fakeConfigRepo.watchEnabled = true;
    fakeConfigRepo.pairs = [];
    await service.start();
    expect(watcherFactory.watchedPathCount, 0);

    await service.updatePairs([pairAmbos]);
    watcherFactory.emit(pairAmbos.pair.localPath);
    await pumpEventQueue();

    expect(timerFactory.createCount, 1);
  });

  test('updatePairs: cambiar la dirección de Ambos a Descargar cancela su watcher', () async {
    fakeConfigRepo.watchEnabled = true;
    fakeConfigRepo.pairs = [pairAmbos];
    await service.start();
    expect(watcherFactory.watchedPathCount, 1);

    final changedToDownload = SyncPairConfig(
      pair: pairAmbos.pair,
      direction: SyncDirection.download,
    );
    await service.updatePairs([changedToDownload]);

    watcherFactory.emit(pairAmbos.pair.localPath);
    await pumpEventQueue();

    expect(timerFactory.createCount, 0);
  });

  test('un error en el stream no detiene la vigilancia de ese par ni de los demás', () async {
    fakeConfigRepo.watchEnabled = true;
    fakeConfigRepo.pairs = [pairAmbos];
    await service.start();

    watcherFactory.emitError(pairAmbos.pair.localPath);
    await pumpEventQueue();

    // El servicio sigue vivo: un evento normal después del error todavía
    // arma su temporizador con normalidad.
    watcherFactory.emit(pairAmbos.pair.localPath);
    await pumpEventQueue();

    expect(timerFactory.createCount, 1);
  });
}
