import 'package:get_it/get_it.dart';

import '../../files/domain/repositories/files_repository.dart';
import '../data/repositories/sync_config_repository_impl.dart';
import '../domain/repositories/sync_config_repository.dart';
import '../domain/services/sync_engine.dart';

void configureSyncDependencies(GetIt sl) {
  sl
    ..registerLazySingleton<SyncConfigRepository>(SyncConfigRepositoryImpl.new)
    ..registerLazySingleton(
      () => SyncEngine(filesRepository: sl<FilesRepository>()),
    );
}
