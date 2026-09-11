import 'package:get_it/get_it.dart';

import '../../auth/domain/repositories/auth_repository.dart';
import '../../files/domain/repositories/files_repository.dart';
import '../data/repositories/file_local_trash_store.dart';
import '../data/repositories/file_sync_state_store.dart';
import '../data/repositories/sync_config_repository_impl.dart';
import '../domain/repositories/local_trash_store.dart';
import '../domain/repositories/sync_config_repository.dart';
import '../domain/repositories/sync_state_store.dart';
import '../domain/services/auto_sync_scheduler.dart';
import '../domain/services/multi_pair_sync_coordinator.dart';
import '../domain/services/sync_engine.dart';

void configureSyncDependencies(GetIt sl) {
  sl
    ..registerLazySingleton<SyncConfigRepository>(SyncConfigRepositoryImpl.new)
    ..registerLazySingleton<SyncStateStore>(FileSyncStateStore.new)
    ..registerLazySingleton<LocalTrashStore>(FileLocalTrashStore.new)
    ..registerLazySingleton(
      () => SyncEngine(
        filesRepository: sl<FilesRepository>(),
        stateStore: sl<SyncStateStore>(),
        trashStore: sl<LocalTrashStore>(),
      ),
    )
    ..registerLazySingleton(
      () => MultiPairSyncCoordinator(syncEngine: sl<SyncEngine>()),
    )
    ..registerLazySingleton(
      () => AutoSyncScheduler(
        coordinator: sl<MultiPairSyncCoordinator>(),
        configRepository: sl<SyncConfigRepository>(),
        authRepository: sl<AuthRepository>(),
      ),
    );
}
