import 'package:get_it/get_it.dart';

import '../../auth/domain/repositories/auth_repository.dart';
import '../../files/domain/repositories/files_repository.dart';
import '../data/repositories/file_sync_state_store.dart';
import '../data/repositories/sync_config_repository_impl.dart';
import '../domain/repositories/sync_config_repository.dart';
import '../domain/repositories/sync_state_store.dart';
import '../domain/services/auto_sync_scheduler.dart';
import '../domain/services/sync_engine.dart';

void configureSyncDependencies(GetIt sl) {
  sl
    ..registerLazySingleton<SyncConfigRepository>(SyncConfigRepositoryImpl.new)
    ..registerLazySingleton<SyncStateStore>(FileSyncStateStore.new)
    ..registerLazySingleton(
      () => SyncEngine(
        filesRepository: sl<FilesRepository>(),
        stateStore: sl<SyncStateStore>(),
      ),
    )
    ..registerLazySingleton(
      () => AutoSyncScheduler(
        syncEngine: sl<SyncEngine>(),
        configRepository: sl<SyncConfigRepository>(),
        authRepository: sl<AuthRepository>(),
      ),
    );
}
