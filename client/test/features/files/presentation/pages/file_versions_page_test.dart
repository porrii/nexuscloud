import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/di/service_locator.dart';
import 'package:nexuscloud_client/core/network/api_exception.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_listing.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_entry.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_version.dart';
import 'package:nexuscloud_client/features/files/domain/repositories/files_repository.dart';
import 'package:nexuscloud_client/features/files/presentation/pages/file_versions_page.dart';

class _FakeFilesRepository implements FilesRepository {
  List<FileVersion> versions = [];
  ApiException? versionsError;
  FileEntry? restoreResult;
  ApiException? restoreError;
  final List<int> restoredVersionNums = [];

  @override
  Future<List<FileVersion>> listVersions(String fileId) async {
    if (versionsError != null) throw versionsError!;
    return versions;
  }

  @override
  Future<FileEntry> restoreVersion({
    required String fileId,
    required int versionNum,
  }) async {
    restoredVersionNums.add(versionNum);
    if (restoreError != null) throw restoreError!;
    // Tras restaurar, la versión restaurada desaparece del historial --
    // igual criterio que `trash_page_test.dart` (mutar lo que la
    // siguiente `listVersions` devuelve), para que el test de "recarga"
    // compruebe algo real y no solo un conteo de llamadas.
    versions = versions.where((v) => v.versionNum != versionNum).toList();
    return restoreResult!;
  }

  // getSaveLocation() pasa por un canal de plataforma real de
  // `file_selector` -- igual que `_downloadFile` en
  // `file_browser_page_test.dart`, ese flujo completo (tocar "Descargar"
  // de verdad) se cubre en la verificación manual, no aquí.
  @override
  Future<void> downloadVersion({
    required String fileId,
    required FileVersion version,
    required String saveToPath,
    TransferProgress? onProgress,
  }) =>
      throw UnimplementedError();

  // Resto de la interfaz -- ajeno al historial de versiones, no lo
  // ejercita ningún test de este archivo.
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
  Future<void> restoreFile(String fileId) => throw UnimplementedError();

  @override
  Future<void> restoreDirectory(String directoryId) => throw UnimplementedError();

  @override
  Future<DirectoryListing> listTrash() => throw UnimplementedError();
}

FileEntry _file({int sizeBytes = 2048, DateTime? updatedAt}) => FileEntry(
      id: 'f1',
      parentPath: '/',
      name: 'informe.pdf',
      sizeBytes: sizeBytes,
      sha256: 'abc',
      mimeType: 'application/pdf',
      createdAt: DateTime.utc(2026),
      updatedAt: updatedAt ?? DateTime.utc(2026, 2, 1, 10, 0),
    );

FileVersion _version({int versionNum = 3, int sizeBytes = 1024}) => FileVersion(
      versionNum: versionNum,
      sizeBytes: sizeBytes,
      sha256: 'def',
      mimeType: 'application/pdf',
      createdAt: DateTime.utc(2026, 1, 15, 9, 30),
    );

void main() {
  setUp(() async {
    // Ver nota en login_page_test.dart: GetIt.reset() es async.
    await sl.reset();
    sl.registerSingleton<FilesRepository>(_FakeFilesRepository());
  });

  testWidgets(
    'muestra "Versión actual" con los datos del archivo y el estado vacío sin versiones anteriores',
    (tester) async {
      final file = _file();
      await tester.pumpWidget(MaterialApp(home: FileVersionsPage(file: file)));
      await tester.pumpAndSettle();

      expect(find.text('Versión actual'), findsOneWidget);
      expect(find.textContaining('2.0 KB'), findsOneWidget);
      expect(find.textContaining('2026-02-01'), findsOneWidget);
      expect(
        find.text('Todavía no hay versiones anteriores de este archivo.'),
        findsOneWidget,
      );
    },
  );

  testWidgets('lista versiones anteriores con su tamaño y fecha', (tester) async {
    final repo = sl<FilesRepository>() as _FakeFilesRepository;
    repo.versions = [_version(versionNum: 3, sizeBytes: 1024)];

    await tester.pumpWidget(MaterialApp(home: FileVersionsPage(file: _file())));
    await tester.pumpAndSettle();

    expect(find.text('Versión 3'), findsOneWidget);
    expect(find.textContaining('1.0 KB'), findsOneWidget);
    expect(find.textContaining('2026-01-15'), findsOneWidget);
    // Presencia de las acciones -- tocar "Descargar" de verdad requiere
    // un canal de plataforma real, ver nota en el fake de arriba.
    expect(find.byTooltip('Descargar'), findsOneWidget);
    expect(find.byTooltip('Restaurar'), findsOneWidget);
  });

  testWidgets(
    'restaurar no pide confirmación, actualiza la versión actual y recarga la lista',
    (tester) async {
      final repo = sl<FilesRepository>() as _FakeFilesRepository;
      repo.versions = [_version(versionNum: 3, sizeBytes: 1024)];
      repo.restoreResult = _file(sizeBytes: 1024, updatedAt: DateTime.utc(2026, 1, 15, 9, 30));

      await tester.pumpWidget(MaterialApp(home: FileVersionsPage(file: _file())));
      await tester.pumpAndSettle();

      await tester.tap(find.byTooltip('Restaurar'));
      await tester.pumpAndSettle();

      // Sin diálogo de confirmación -- ninguno de los dos botones que
      // usaría (confirm_dialog.dart) debería haber aparecido.
      expect(find.byType(AlertDialog), findsNothing);

      expect(repo.restoredVersionNums, [3]);
      // "Versión actual" ya refleja el `FileEntry` devuelto por el
      // restore (1.0 KB), no el original (2.0 KB).
      expect(find.textContaining('1.0 KB'), findsOneWidget);
      // La lista se recargó de verdad: la versión restaurada ya no
      // aparece como anterior.
      expect(find.text('Versión 3'), findsNothing);
      expect(
        find.text('Todavía no hay versiones anteriores de este archivo.'),
        findsOneWidget,
      );
    },
  );

  testWidgets(
    'un error de red al restaurar muestra el mensaje de error en vez de crashear',
    (tester) async {
      final repo = sl<FilesRepository>() as _FakeFilesRepository;
      repo.versions = [_version(versionNum: 3)];
      repo.restoreError = const ApiException(
        code: 'not_found',
        message: 'Versión no encontrada.',
      );

      await tester.pumpWidget(MaterialApp(home: FileVersionsPage(file: _file())));
      await tester.pumpAndSettle();

      await tester.tap(find.byTooltip('Restaurar'));
      await tester.pumpAndSettle();

      expect(find.text('Versión no encontrada.'), findsOneWidget);
      expect(find.text('Reintentar'), findsOneWidget);
    },
  );
}
