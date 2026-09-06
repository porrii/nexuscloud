import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/di/service_locator.dart';
import 'package:nexuscloud_client/core/network/api_exception.dart';
import 'package:nexuscloud_client/features/auth/domain/entities/app_user.dart';
import 'package:nexuscloud_client/features/auth/domain/entities/auto_login_outcome.dart';
import 'package:nexuscloud_client/features/auth/domain/entities/login_result.dart';
import 'package:nexuscloud_client/features/auth/domain/repositories/auth_repository.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_entry.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_listing.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_entry.dart';
import 'package:nexuscloud_client/features/files/domain/repositories/files_repository.dart';
import 'package:nexuscloud_client/features/files/presentation/pages/file_browser_page.dart';

class _FakeAuthRepository implements AuthRepository {
  @override
  AppUser? get currentUser => null;

  @override
  Stream<AppUser?> get userStream => const Stream.empty();

  @override
  Future<AutoLoginOutcome> tryAutoLogin() async =>
      AutoLoginOutcome.noSavedSession;

  @override
  Future<LoginResult> login({
    required String serverBaseUrl,
    required String username,
    required String password,
    String? totpCode,
  }) async =>
      throw UnimplementedError();

  @override
  Future<void> logout() async {}
}

class _FakeFilesRepository implements FilesRepository {
  final Map<String, DirectoryListing> listingsByPath = {};
  final List<String> requestedPaths = [];
  ApiException? errorToThrow;

  @override
  Future<DirectoryListing> list(String path) async {
    requestedPaths.add(path);
    if (errorToThrow != null) throw errorToThrow!;
    return listingsByPath[path] ??
        const DirectoryListing(directories: [], files: []);
  }

  // uploadFile/downloadFile pasan por `file_selector` (un canal de
  // plataforma real) antes de llegar aquí -- ese flujo completo se cubre
  // en la verificación manual (ADR-010), no en este widget test. Aquí
  // solo hace falta satisfacer la interfaz.
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

  final List<String> deletedFileIds = [];
  final List<String> deletedDirectoryIds = [];
  ApiException? deleteError;

  @override
  Future<void> deleteFile(String fileId, {bool permanent = false}) async {
    if (deleteError != null) throw deleteError!;
    deletedFileIds.add(fileId);
  }

  @override
  Future<void> deleteDirectory(String directoryId, {bool permanent = false}) async {
    if (deleteError != null) throw deleteError!;
    deletedDirectoryIds.add(directoryId);
  }

  // restoreFile/restoreDirectory/listTrash son responsabilidad de
  // TrashPage, no de FileBrowserPage -- no los ejercita ningún test de
  // este archivo.
  @override
  Future<void> restoreFile(String fileId) => throw UnimplementedError();

  @override
  Future<void> restoreDirectory(String directoryId) => throw UnimplementedError();

  @override
  Future<DirectoryListing> listTrash() => throw UnimplementedError();
}

void main() {
  setUp(() async {
    // Ver nota en login_page_test.dart: GetIt.reset() es async y hay que
    // esperarlo antes de volver a registrar, o la limpieza puede pisar el
    // registro de forma intermitente.
    await sl.reset();
    sl.registerSingleton<AuthRepository>(_FakeAuthRepository());
    sl.registerSingleton<FilesRepository>(_FakeFilesRepository());
  });

  testWidgets('muestra el estado vacío cuando la carpeta no tiene contenido',
      (tester) async {
    await tester.pumpWidget(const MaterialApp(home: FileBrowserPage()));
    await tester.pumpAndSettle();

    expect(find.text('Esta carpeta está vacía'), findsOneWidget);
  });

  testWidgets('lista carpetas y archivos, navega al tocar una carpeta',
      (tester) async {
    final filesRepo = sl<FilesRepository>() as _FakeFilesRepository;
    filesRepo.listingsByPath['/'] = DirectoryListing(
      directories: [
        DirectoryEntry(
          id: 'd1',
          parentPath: '/',
          name: 'Fotos',
          createdAt: DateTime.utc(2026),
        ),
      ],
      files: [
        FileEntry(
          id: 'f1',
          parentPath: '/',
          name: 'informe.pdf',
          sizeBytes: 2048,
          sha256: 'abc',
          mimeType: 'application/pdf',
          createdAt: DateTime.utc(2026),
          updatedAt: DateTime.utc(2026),
        ),
      ],
    );
    filesRepo.listingsByPath['/Fotos'] =
        const DirectoryListing(directories: [], files: []);

    await tester.pumpWidget(const MaterialApp(home: FileBrowserPage()));
    await tester.pumpAndSettle();

    expect(find.text('Fotos'), findsOneWidget);
    expect(find.text('informe.pdf'), findsOneWidget);

    await tester.tap(find.text('Fotos'));
    await tester.pumpAndSettle();

    expect(filesRepo.requestedPaths, contains('/Fotos'));
    expect(find.text('Esta carpeta está vacía'), findsOneWidget);
  });

  testWidgets(
    'muestra la acción de subir en la AppBar y de descargar por archivo',
    (tester) async {
      final filesRepo = sl<FilesRepository>() as _FakeFilesRepository;
      filesRepo.listingsByPath['/'] = DirectoryListing(
        directories: const [],
        files: [
          FileEntry(
            id: 'f1',
            parentPath: '/',
            name: 'informe.pdf',
            sizeBytes: 2048,
            sha256: 'abc',
            mimeType: 'application/pdf',
            createdAt: DateTime.utc(2026),
            updatedAt: DateTime.utc(2026),
          ),
        ],
      );

      await tester.pumpWidget(const MaterialApp(home: FileBrowserPage()));
      await tester.pumpAndSettle();

      expect(find.byTooltip('Subir archivo'), findsOneWidget);
      expect(find.byTooltip('Descargar'), findsOneWidget);
      expect(find.byTooltip('Sincronización'), findsOneWidget);
    },
  );

  testWidgets('muestra un banner de error y permite reintentar', (tester) async {
    final filesRepo = sl<FilesRepository>() as _FakeFilesRepository;
    filesRepo.errorToThrow = const ApiException(
      code: 'forbidden',
      message: 'Acceso no permitido.',
    );

    await tester.pumpWidget(const MaterialApp(home: FileBrowserPage()));
    await tester.pumpAndSettle();

    expect(find.text('Acceso no permitido.'), findsOneWidget);
    expect(find.text('Reintentar'), findsOneWidget);
  });

  testWidgets('borra un archivo tras confirmar y recarga el listado', (tester) async {
    final filesRepo = sl<FilesRepository>() as _FakeFilesRepository;
    filesRepo.listingsByPath['/'] = DirectoryListing(
      directories: const [],
      files: [
        FileEntry(
          id: 'f1',
          parentPath: '/',
          name: 'borrame.txt',
          sizeBytes: 10,
          sha256: 'abc',
          mimeType: 'text/plain',
          createdAt: DateTime.utc(2026),
          updatedAt: DateTime.utc(2026),
        ),
      ],
    );

    await tester.pumpWidget(const MaterialApp(home: FileBrowserPage()));
    await tester.pumpAndSettle();

    await tester.tap(find.byTooltip('Eliminar'));
    await tester.pumpAndSettle();

    expect(find.text('Mover a la papelera'), findsWidgets); // título + botón

    await tester.tap(find.widgetWithText(FilledButton, 'Mover a la papelera'));
    await tester.pumpAndSettle();

    expect(filesRepo.deletedFileIds, ['f1']);
  });

  testWidgets(
    'el error not_empty al borrar una carpeta muestra un mensaje claro, no el crudo',
    (tester) async {
      final filesRepo = sl<FilesRepository>() as _FakeFilesRepository;
      filesRepo.listingsByPath['/'] = DirectoryListing(
        directories: [
          DirectoryEntry(
            id: 'd1',
            parentPath: '/',
            name: 'NoVacia',
            createdAt: DateTime.utc(2026),
          ),
        ],
        files: const [],
      );
      filesRepo.deleteError = const ApiException(
        code: 'not_empty',
        message: 'mensaje crudo del servidor',
      );

      await tester.pumpWidget(const MaterialApp(home: FileBrowserPage()));
      await tester.pumpAndSettle();

      await tester.tap(find.byTooltip('Eliminar'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'Mover a la papelera'));
      await tester.pumpAndSettle();

      expect(
        find.text('Esa carpeta no está vacía: elimina primero su contenido.'),
        findsOneWidget,
      );
      expect(find.text('mensaje crudo del servidor'), findsNothing);
    },
  );
}
