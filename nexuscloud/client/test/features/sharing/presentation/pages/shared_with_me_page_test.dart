import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/di/service_locator.dart';
import 'package:nexuscloud_client/core/network/api_exception.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_entry.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_listing.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_entry.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_version.dart';
import 'package:nexuscloud_client/features/files/domain/repositories/files_repository.dart';
import 'package:nexuscloud_client/features/sharing/domain/entities/group.dart';
import 'package:nexuscloud_client/features/sharing/domain/entities/share.dart';
import 'package:nexuscloud_client/features/sharing/domain/repositories/sharing_repository.dart';
import 'package:nexuscloud_client/features/sharing/presentation/pages/shared_with_me_page.dart';

/// A diferencia del fake ya existente en `share_page_test.dart`/
/// `my_shares_page_test.dart` (que ignoran por completo el argumento
/// `direction`), este SÍ lo registra -- aquí importa de verdad: un
/// copy-paste que llamara por error a `direction: byMe` debe hacer
/// fallar un test, no pasar en silencio. `listSharedDirectory` sigue el
/// mismo patrón que `_FakeFilesRepository` en `file_browser_page_test.dart`
/// (`Map` por clave + lista de IDs pedidos), aquí por ID de carpeta en
/// vez de por ruta.
class _FakeSharingRepository implements SharingRepository {
  List<Share> withMeShares = [];
  final List<ShareDirection> requestedDirections = [];

  final Map<String, DirectoryListing> listingsByDirectoryId = {};
  final List<String> requestedDirectoryIds = [];

  // Subida a carpeta compartida (§37, ADR-035): `uploadCalls` guarda cada
  // llamada para comprobar a qué carpeta y con qué nombre se subió;
  // `uploadError` simula el rechazo del servidor (p.ej. `upload_not_allowed`).
  final List<Map<String, String>> uploadCalls = [];
  ApiException? uploadError;

  @override
  Future<List<Share>> listShares({required ShareDirection direction}) async {
    requestedDirections.add(direction);
    return withMeShares;
  }

  @override
  Future<DirectoryListing> listSharedDirectory(String directoryId) async {
    requestedDirectoryIds.add(directoryId);
    return listingsByDirectoryId[directoryId] ??
        const DirectoryListing(directories: [], files: []);
  }

  @override
  Future<FileEntry> uploadToSharedDirectory({
    required String directoryId,
    required String localFilePath,
    required String fileName,
    TransferProgress? onProgress,
  }) async {
    uploadCalls.add({'directoryId': directoryId, 'fileName': fileName});
    onProgress?.call(1, 1);
    if (uploadError != null) throw uploadError!;
    return FileEntry(
      id: 'nuevo-$fileName',
      parentPath: '/',
      name: fileName,
      sizeBytes: 1,
      sha256: 'x',
      mimeType: 'application/octet-stream',
      createdAt: DateTime.utc(2026),
      updatedAt: DateTime.utc(2026),
    );
  }

  @override
  Future<void> downloadSharedFile({
    required String fileId,
    required String saveToPath,
    TransferProgress? onProgress,
  }) =>
      throw UnimplementedError();

  // Ajenos a "Compartido conmigo" -- no los ejercita ningún test de este
  // archivo.
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
  Future<void> revokeShare(String shareId) => throw UnimplementedError();

  @override
  Future<String?> get serverBaseUrl => throw UnimplementedError();
}

class _FakeFilesRepository implements FilesRepository {
  @override
  Future<void> downloadFile({
    required FileEntry file,
    required String saveToPath,
    TransferProgress? onProgress,
  }) =>
      throw UnimplementedError();

  // El resto de la interfaz -- ajena a esta página, que solo usa
  // `downloadFile` (y ni eso, en los tests: tocar "Descargar" de verdad
  // pasa por `getSaveLocation()`, un canal de plataforma real -- ese flujo
  // completo se cubre en la verificación manual, mismo criterio ya
  // establecido en el resto del cliente).
  @override
  Future<DirectoryListing> list(String path) => throw UnimplementedError();

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

Share _directoryShare({
  String id = 's1',
  String resourceId = 'd1',
  String? resourceName = 'Documentos',
}) =>
    Share(
      id: id,
      resourceType: ShareResourceType.directory,
      resourceId: resourceId,
      resourceName: resourceName,
      shareType: ShareType.user,
      canDownload: true,
      canUpload: false,
      hasPassword: false,
      downloadCount: 0,
      createdAt: DateTime.utc(2026),
    );

Share _fileShare({
  String id = 's2',
  String resourceId = 'f1',
  String? resourceName = 'informe.pdf',
}) =>
    Share(
      id: id,
      resourceType: ShareResourceType.file,
      resourceId: resourceId,
      resourceName: resourceName,
      shareType: ShareType.user,
      canDownload: true,
      canUpload: false,
      hasPassword: false,
      downloadCount: 0,
      createdAt: DateTime.utc(2026),
    );

void main() {
  setUp(() async {
    // Ver nota en login_page_test.dart: GetIt.reset() es async.
    await sl.reset();
    sl.registerSingleton<SharingRepository>(_FakeSharingRepository());
    sl.registerSingleton<FilesRepository>(_FakeFilesRepository());
  });

  testWidgets('muestra el estado vacío de la lista plana', (tester) async {
    await tester.pumpWidget(const MaterialApp(home: SharedWithMePage()));
    await tester.pumpAndSettle();

    expect(find.text('Nadie ha compartido nada contigo todavía'), findsOneWidget);
  });

  testWidgets('siempre pide direction: withMe, nunca byMe', (tester) async {
    final repo = sl<SharingRepository>() as _FakeSharingRepository;

    await tester.pumpWidget(const MaterialApp(home: SharedWithMePage()));
    await tester.pumpAndSettle();

    expect(repo.requestedDirections, [ShareDirection.withMe]);
  });

  testWidgets(
    'lista comparticiones recibidas con su icono/tipo/etiqueta correctos',
    (tester) async {
      final repo = sl<SharingRepository>() as _FakeSharingRepository;
      repo.withMeShares = [_directoryShare(), _fileShare()];

      await tester.pumpWidget(const MaterialApp(home: SharedWithMePage()));
      await tester.pumpAndSettle();

      expect(find.text('Documentos'), findsOneWidget);
      expect(find.text('Carpeta compartida'), findsOneWidget);
      expect(find.text('informe.pdf'), findsOneWidget);
      expect(find.text('Archivo compartido'), findsOneWidget);
      // Solo el archivo tiene acción de Descargar en la lista plana.
      expect(find.byTooltip('Descargar'), findsOneWidget);
    },
  );

  testWidgets('tocar una carpeta navega dentro y muestra su contenido',
      (tester) async {
    final repo = sl<SharingRepository>() as _FakeSharingRepository;
    repo.withMeShares = [_directoryShare(resourceId: 'd1', resourceName: 'Documentos')];
    repo.listingsByDirectoryId['d1'] = DirectoryListing(
      directories: const [],
      files: [
        FileEntry(
          id: 'f1',
          parentPath: '/Documentos',
          name: 'contrato.pdf',
          sizeBytes: 2048,
          sha256: 'abc',
          mimeType: 'application/pdf',
          createdAt: DateTime.utc(2026),
          updatedAt: DateTime.utc(2026),
        ),
      ],
    );

    await tester.pumpWidget(const MaterialApp(home: SharedWithMePage()));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Documentos'));
    await tester.pumpAndSettle();

    expect(find.text('contrato.pdf'), findsOneWidget);
    expect(find.text('2.0 KB'), findsOneWidget);
    expect(repo.requestedDirectoryIds, ['d1']);
  });

  testWidgets('una carpeta compartida vacía muestra el texto distinto',
      (tester) async {
    final repo = sl<SharingRepository>() as _FakeSharingRepository;
    repo.withMeShares = [_directoryShare(resourceId: 'd1')];
    repo.listingsByDirectoryId['d1'] =
        const DirectoryListing(directories: [], files: []);

    await tester.pumpWidget(const MaterialApp(home: SharedWithMePage()));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Documentos'));
    await tester.pumpAndSettle();

    expect(find.text('Esta carpeta está vacía.'), findsOneWidget);
  });

  testWidgets(
    'el botón de subir no aparece en la lista plana (sin navegar dentro)',
    (tester) async {
      final repo = sl<SharingRepository>() as _FakeSharingRepository;
      repo.withMeShares = [_directoryShare(resourceId: 'd1')];
      repo.listingsByDirectoryId['d1'] =
          const DirectoryListing(directories: [], files: [], canUpload: true);

      await tester.pumpWidget(const MaterialApp(home: SharedWithMePage()));
      await tester.pumpAndSettle();

      expect(find.byTooltip('Subir archivo'), findsNothing);
    },
  );

  testWidgets(
    'el botón de subir no aparece si la carpeta no permite subir',
    (tester) async {
      final repo = sl<SharingRepository>() as _FakeSharingRepository;
      repo.withMeShares = [_directoryShare(resourceId: 'd1')];
      repo.listingsByDirectoryId['d1'] =
          const DirectoryListing(directories: [], files: []); // canUpload: false por defecto

      await tester.pumpWidget(const MaterialApp(home: SharedWithMePage()));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Documentos'));
      await tester.pumpAndSettle();

      expect(find.byTooltip('Subir archivo'), findsNothing);
    },
  );

  testWidgets(
    'el botón de subir aparece al navegar dentro de una carpeta con permiso de subida',
    (tester) async {
      final repo = sl<SharingRepository>() as _FakeSharingRepository;
      repo.withMeShares = [_directoryShare(resourceId: 'd1')];
      repo.listingsByDirectoryId['d1'] =
          const DirectoryListing(directories: [], files: [], canUpload: true);

      await tester.pumpWidget(const MaterialApp(home: SharedWithMePage()));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Documentos'));
      await tester.pumpAndSettle();

      expect(find.byTooltip('Subir archivo'), findsOneWidget);
    },
  );

  testWidgets(
    'volver a la raíz por la migaja oculta de nuevo el botón de subir',
    (tester) async {
      final repo = sl<SharingRepository>() as _FakeSharingRepository;
      repo.withMeShares = [_directoryShare(resourceId: 'd1')];
      repo.listingsByDirectoryId['d1'] =
          const DirectoryListing(directories: [], files: [], canUpload: true);

      await tester.pumpWidget(const MaterialApp(home: SharedWithMePage()));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Documentos'));
      await tester.pumpAndSettle();
      expect(find.byTooltip('Subir archivo'), findsOneWidget);

      await tester.tap(find.text('Compartido conmigo'));
      await tester.pumpAndSettle();

      expect(find.byTooltip('Subir archivo'), findsNothing);
    },
  );

  testWidgets('navegar dos niveles de profundidad (carpeta dentro de carpeta)',
      (tester) async {
    final repo = sl<SharingRepository>() as _FakeSharingRepository;
    repo.withMeShares = [_directoryShare(resourceId: 'd1', resourceName: 'Documentos')];
    repo.listingsByDirectoryId['d1'] = DirectoryListing(
      directories: [
        DirectoryEntry(
          id: 'd2',
          parentPath: '/Documentos',
          name: 'Contratos',
          createdAt: DateTime.utc(2026),
        ),
      ],
      files: const [],
    );
    repo.listingsByDirectoryId['d2'] = DirectoryListing(
      directories: const [],
      files: [
        FileEntry(
          id: 'f1',
          parentPath: '/Documentos/Contratos',
          name: 'anexo.pdf',
          sizeBytes: 512,
          sha256: 'def',
          mimeType: 'application/pdf',
          createdAt: DateTime.utc(2026),
          updatedAt: DateTime.utc(2026),
        ),
      ],
    );

    await tester.pumpWidget(const MaterialApp(home: SharedWithMePage()));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Documentos'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Contratos'));
    await tester.pumpAndSettle();

    expect(find.text('anexo.pdf'), findsOneWidget);
    expect(repo.requestedDirectoryIds, ['d1', 'd2']);
  });

  testWidgets(
    'volver a la raíz por la migaja NO vuelve a llamar a listShares',
    (tester) async {
      final repo = sl<SharingRepository>() as _FakeSharingRepository;
      repo.withMeShares = [_directoryShare(resourceId: 'd1', resourceName: 'Documentos')];
      repo.listingsByDirectoryId['d1'] =
          const DirectoryListing(directories: [], files: []);

      await tester.pumpWidget(const MaterialApp(home: SharedWithMePage()));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Documentos'));
      await tester.pumpAndSettle();

      expect(repo.requestedDirections.length, 1); // solo la carga inicial

      await tester.tap(find.text('Compartido conmigo'));
      await tester.pumpAndSettle();

      expect(find.text('Documentos'), findsOneWidget);
      expect(find.text('Nadie ha compartido nada contigo todavía'), findsNothing);
      // Sigue siendo 1: volver a la raíz no repite la llamada de red, la
      // lista plana ya estaba en memoria.
      expect(repo.requestedDirections.length, 1);
    },
  );
}
