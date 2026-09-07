import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/network/api_exception.dart';
import 'package:nexuscloud_client/core/storage/server_config_store.dart';
import 'package:nexuscloud_client/features/sharing/data/datasources/sharing_remote_data_source.dart';
import 'package:nexuscloud_client/features/sharing/data/repositories/sharing_repository_impl.dart';
import 'package:nexuscloud_client/features/sharing/domain/entities/group.dart';
import 'package:nexuscloud_client/features/sharing/domain/entities/share.dart';

class _FakeSharingRemoteDataSource implements SharingRemoteDataSource {
  List<Group>? groups;
  ApiException? groupsError;

  Share? createResult;
  ApiException? createError;
  final List<Map<String, dynamic>> createCalls = [];

  List<Share>? shares;
  ApiException? sharesError;
  final List<ShareDirection> listCalls = [];

  ApiException? revokeError;
  final List<String> revokedIds = [];

  @override
  Future<List<Group>> listGroups() async {
    if (groupsError != null) throw groupsError!;
    return groups ?? const [];
  }

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
  }) async {
    createCalls.add({
      'resourceType': resourceType,
      'resourceId': resourceId,
      'shareType': shareType,
      'targetUsername': targetUsername,
      'targetGroupId': targetGroupId,
      'expiresAt': expiresAt,
    });
    if (createError != null) throw createError!;
    return createResult!;
  }

  @override
  Future<List<Share>> listShares({required ShareDirection direction}) async {
    listCalls.add(direction);
    if (sharesError != null) throw sharesError!;
    return shares ?? const [];
  }

  @override
  Future<void> revokeShare(String shareId) async {
    if (revokeError != null) throw revokeError!;
    revokedIds.add(shareId);
  }
}

class _FakeServerConfigStore implements ServerConfigStore {
  String? url;

  @override
  Future<void> save(String baseUrl) async => url = baseUrl;

  @override
  Future<String?> read() async => url;

  @override
  Future<void> clear() async => url = null;
}

void main() {
  final testShare = Share(
    id: 's1',
    resourceType: ShareResourceType.file,
    resourceId: 'f1',
    shareType: ShareType.user,
    canDownload: true,
    canUpload: false,
    hasPassword: false,
    downloadCount: 0,
    createdAt: DateTime.utc(2026),
  );

  test('listGroups delega y devuelve tal cual el resultado', () async {
    final fake = _FakeSharingRemoteDataSource()
      ..groups = const [Group(id: 'g1', name: 'Equipo')];
    final repo = SharingRepositoryImpl(
      remoteDataSource: fake,
      serverConfigStore: _FakeServerConfigStore(),
    );

    final result = await repo.listGroups();

    expect(result, const [Group(id: 'g1', name: 'Equipo')]);
  });

  test('una ApiException de listGroups se propaga sin cambios', () async {
    final fake = _FakeSharingRemoteDataSource()
      ..groupsError = const ApiException(code: 'forbidden', message: 'x');
    final repo = SharingRepositoryImpl(
      remoteDataSource: fake,
      serverConfigStore: _FakeServerConfigStore(),
    );

    await expectLater(
      repo.listGroups(),
      throwsA(isA<ApiException>().having((e) => e.code, 'code', 'forbidden')),
    );
  });

  test('createShare delega con los parámetros correctos y devuelve el Share creado',
      () async {
    final fake = _FakeSharingRemoteDataSource()..createResult = testShare;
    final repo = SharingRepositoryImpl(
      remoteDataSource: fake,
      serverConfigStore: _FakeServerConfigStore(),
    );

    final result = await repo.createShare(
      resourceType: ShareResourceType.file,
      resourceId: 'f1',
      shareType: ShareType.user,
      targetUsername: 'alice',
    );

    expect(result, testShare);
    expect(fake.createCalls, [
      {
        'resourceType': ShareResourceType.file,
        'resourceId': 'f1',
        'shareType': ShareType.user,
        'targetUsername': 'alice',
        'targetGroupId': null,
        'expiresAt': null,
      },
    ]);
  });

  test('una ApiException de createShare (p.ej. invalid_request) se propaga',
      () async {
    final fake = _FakeSharingRemoteDataSource()
      ..createError = const ApiException(code: 'invalid_request', message: 'x');
    final repo = SharingRepositoryImpl(
      remoteDataSource: fake,
      serverConfigStore: _FakeServerConfigStore(),
    );

    await expectLater(
      repo.createShare(
        resourceType: ShareResourceType.file,
        resourceId: 'f1',
        shareType: ShareType.user,
        targetUsername: 'no-existe',
      ),
      throwsA(
        isA<ApiException>().having((e) => e.code, 'code', 'invalid_request'),
      ),
    );
  });

  test('listShares delega la dirección y devuelve tal cual el resultado', () async {
    final fake = _FakeSharingRemoteDataSource()..shares = [testShare];
    final repo = SharingRepositoryImpl(
      remoteDataSource: fake,
      serverConfigStore: _FakeServerConfigStore(),
    );

    final result = await repo.listShares(direction: ShareDirection.byMe);

    expect(result, [testShare]);
    expect(fake.listCalls, [ShareDirection.byMe]);
  });

  test('una ApiException de listShares se propaga sin cambios', () async {
    final fake = _FakeSharingRemoteDataSource()
      ..sharesError = const ApiException(code: 'internal_error', message: 'x');
    final repo = SharingRepositoryImpl(
      remoteDataSource: fake,
      serverConfigStore: _FakeServerConfigStore(),
    );

    await expectLater(
      repo.listShares(direction: ShareDirection.byMe),
      throwsA(
        isA<ApiException>().having((e) => e.code, 'code', 'internal_error'),
      ),
    );
  });

  test('revokeShare delega con el id correcto', () async {
    final fake = _FakeSharingRemoteDataSource();
    final repo = SharingRepositoryImpl(
      remoteDataSource: fake,
      serverConfigStore: _FakeServerConfigStore(),
    );

    await repo.revokeShare('s1');

    expect(fake.revokedIds, ['s1']);
  });

  test('una ApiException de revokeShare (p.ej. not_found) se propaga', () async {
    final fake = _FakeSharingRemoteDataSource()
      ..revokeError = const ApiException(code: 'not_found', message: 'x');
    final repo = SharingRepositoryImpl(
      remoteDataSource: fake,
      serverConfigStore: _FakeServerConfigStore(),
    );

    await expectLater(
      repo.revokeShare('s1'),
      throwsA(isA<ApiException>().having((e) => e.code, 'code', 'not_found')),
    );
  });

  test('serverBaseUrl delega en ServerConfigStore.read()', () async {
    final configStore = _FakeServerConfigStore()..url = 'https://midominio.com';
    final repo = SharingRepositoryImpl(
      remoteDataSource: _FakeSharingRemoteDataSource(),
      serverConfigStore: configStore,
    );

    expect(await repo.serverBaseUrl, 'https://midominio.com');
  });
}
