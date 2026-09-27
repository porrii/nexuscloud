import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/di/service_locator.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_listing.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_entry.dart';
import 'package:nexuscloud_client/features/files/domain/repositories/files_repository.dart' show TransferProgress;
import 'package:nexuscloud_client/features/sharing/domain/entities/group.dart';
import 'package:nexuscloud_client/features/sharing/domain/entities/share.dart';
import 'package:nexuscloud_client/features/sharing/domain/repositories/sharing_repository.dart';
import 'package:nexuscloud_client/features/sharing/presentation/pages/my_shares_page.dart';

class _FakeSharingRepository implements SharingRepository {
  List<Share> allShares = [];
  final List<String> revokedIds = [];

  @override
  Future<List<Group>> listGroups() => throw UnimplementedError();

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
      throw UnimplementedError();

  @override
  Future<List<Share>> listShares({required ShareDirection direction}) async =>
      allShares;

  @override
  Future<void> revokeShare(String shareId) async {
    revokedIds.add(shareId);
    allShares = allShares.where((s) => s.id != shareId).toList();
  }

  @override
  Future<String?> get serverBaseUrl => throw UnimplementedError();

  @override
  Future<DirectoryListing> listSharedDirectory(String directoryId) =>
      throw UnimplementedError();

  @override
  Future<FileEntry> uploadToSharedDirectory({
    required String directoryId,
    required String localFilePath,
    required String fileName,
    TransferProgress? onProgress,
  }) =>
      throw UnimplementedError();

  @override
  Future<void> downloadSharedFile({
    required String fileId,
    required String saveToPath,
    TransferProgress? onProgress,
  }) =>
      throw UnimplementedError();
}

void main() {
  setUp(() async {
    // Ver nota en login_page_test.dart: GetIt.reset() es async.
    await sl.reset();
    sl.registerSingleton<SharingRepository>(_FakeSharingRepository());
  });

  testWidgets('muestra el estado vacío cuando no se ha compartido nada',
      (tester) async {
    await tester.pumpWidget(const MaterialApp(home: MySharesPage()));
    await tester.pumpAndSettle();

    expect(find.text('Todavía no has compartido nada'), findsOneWidget);
  });

  testWidgets('lista comparticiones de varios recursos con su nombre y tipo',
      (tester) async {
    final repo = sl<SharingRepository>() as _FakeSharingRepository;
    repo.allShares = [
      Share(
        id: 's1',
        resourceType: ShareResourceType.file,
        resourceId: 'f1',
        resourceName: 'informe.pdf',
        shareType: ShareType.user,
        targetUsername: 'alice',
        canDownload: true,
        canUpload: false,
        hasPassword: false,
        downloadCount: 0,
        createdAt: DateTime.utc(2026, 2, 1),
      ),
      Share(
        id: 's2',
        resourceType: ShareResourceType.directory,
        resourceId: 'd1',
        resourceName: 'Documentos',
        shareType: ShareType.link,
        label: 'Para el equipo',
        canDownload: true,
        canUpload: true,
        hasPassword: true,
        downloadCount: 3,
        maxDownloads: 10,
        createdAt: DateTime.utc(2026, 2, 2),
      ),
    ];

    await tester.pumpWidget(const MaterialApp(home: MySharesPage()));
    await tester.pumpAndSettle();

    expect(find.text('informe.pdf'), findsOneWidget);
    expect(find.text('Documentos'), findsOneWidget);
    expect(find.textContaining('Usuario: alice'), findsOneWidget);
    expect(find.textContaining('Para el equipo'), findsOneWidget);
    expect(find.textContaining('3/10 descargas'), findsOneWidget);
  });

  testWidgets('revocar pide confirmación y, tras confirmar, recarga la lista',
      (tester) async {
    final repo = sl<SharingRepository>() as _FakeSharingRepository;
    repo.allShares = [
      Share(
        id: 's1',
        resourceType: ShareResourceType.file,
        resourceId: 'f1',
        resourceName: 'informe.pdf',
        shareType: ShareType.user,
        targetUsername: 'alice',
        canDownload: true,
        canUpload: false,
        hasPassword: false,
        downloadCount: 0,
        createdAt: DateTime.utc(2026),
      ),
    ];

    await tester.pumpWidget(const MaterialApp(home: MySharesPage()));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Revocar'));
    await tester.pumpAndSettle();

    expect(find.text('Revocar compartición'), findsWidgets);
    expect(repo.revokedIds, isEmpty);

    await tester.tap(find.widgetWithText(FilledButton, 'Revocar'));
    await tester.pumpAndSettle();

    expect(repo.revokedIds, ['s1']);
    expect(find.text('Todavía no has compartido nada'), findsOneWidget);
  });
}
