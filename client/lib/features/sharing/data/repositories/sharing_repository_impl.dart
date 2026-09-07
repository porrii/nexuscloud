import '../../../../core/storage/server_config_store.dart';
import '../../domain/entities/group.dart';
import '../../domain/entities/share.dart';
import '../../domain/repositories/sharing_repository.dart';
import '../datasources/sharing_remote_data_source.dart';

class SharingRepositoryImpl implements SharingRepository {
  SharingRepositoryImpl({
    required SharingRemoteDataSource remoteDataSource,
    required ServerConfigStore serverConfigStore,
  })  : _remoteDataSource = remoteDataSource,
        _serverConfigStore = serverConfigStore;

  final SharingRemoteDataSource _remoteDataSource;
  final ServerConfigStore _serverConfigStore;

  @override
  Future<List<Group>> listGroups() => _remoteDataSource.listGroups();

  @override
  Future<Share> createShare({
    required ShareResourceType resourceType,
    required String resourceId,
    required ShareType shareType,
    String? targetUsername,
    String? targetGroupId,
    String? label,
    bool? canDownload,
    bool canUpload = false,
    String? password,
    DateTime? expiresAt,
    int? maxDownloads,
  }) =>
      _remoteDataSource.createShare(
        resourceType: resourceType,
        resourceId: resourceId,
        shareType: shareType,
        targetUsername: targetUsername,
        targetGroupId: targetGroupId,
        label: label,
        canDownload: canDownload,
        canUpload: canUpload,
        password: password,
        expiresAt: expiresAt,
        maxDownloads: maxDownloads,
      );

  @override
  Future<List<Share>> listShares({required ShareDirection direction}) =>
      _remoteDataSource.listShares(direction: direction);

  @override
  Future<void> revokeShare(String shareId) =>
      _remoteDataSource.revokeShare(shareId);

  @override
  Future<String?> get serverBaseUrl => _serverConfigStore.read();
}
