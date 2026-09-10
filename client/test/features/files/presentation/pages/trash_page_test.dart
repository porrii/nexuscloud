import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/di/service_locator.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_entry.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_listing.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_entry.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_version.dart';
import 'package:nexuscloud_client/features/files/domain/repositories/files_repository.dart';
import 'package:nexuscloud_client/features/files/presentation/pages/trash_page.dart';

class _FakeFilesRepository implements FilesRepository {
  DirectoryListing trashListing = const DirectoryListing(directories: [], files: []);
  final List<String> restoredFileIds = [];
  final List<String> restoredDirectoryIds = [];
  final List<bool> deletedFilePermanentFlags = [];

  @override
  Future<DirectoryListing> listTrash() async => trashListing;

  @override
  Future<void> restoreFile(String fileId) async {
    restoredFileIds.add(fileId);
    // Tras restaurar, ya no debería aparecer en la papelera -- refleja
    // eso en el fake para poder comprobar la recarga.
    trashListing = DirectoryListing(
      directories: trashListing.directories,
      files: trashListing.files.where((f) => f.id != fileId).toList(),
    );
  }

  @override
  Future<void> restoreDirectory(String directoryId) async {
    restoredDirectoryIds.add(directoryId);
    trashListing = DirectoryListing(
      directories:
          trashListing.directories.where((d) => d.id != directoryId).toList(),
      files: trashListing.files,
    );
  }

  @override
  Future<void> deleteFile(String fileId, {bool permanent = false}) async {
    deletedFilePermanentFlags.add(permanent);
    trashListing = DirectoryListing(
      directories: trashListing.directories,
      files: trashListing.files.where((f) => f.id != fileId).toList(),
    );
  }

  @override
  Future<void> deleteDirectory(String directoryId, {bool permanent = false}) =>
      throw UnimplementedError();

  // Ajenos a la papelera -- no los ejercita ningún test de este archivo.
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
  Future<void> downloadFile({
    required FileEntry file,
    required String saveToPath,
    TransferProgress? onProgress,
  }) =>
      throw UnimplementedError();

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

FileEntry _trashedFile({required String id, required String name}) => FileEntry(
      id: id,
      parentPath: '/',
      name: name,
      sizeBytes: 10,
      sha256: 'abc',
      mimeType: 'text/plain',
      createdAt: DateTime.utc(2026),
      updatedAt: DateTime.utc(2026),
      deletedAt: DateTime.utc(2026, 2, 1, 10, 30),
    );

void main() {
  setUp(() async {
    // Ver nota en login_page_test.dart: GetIt.reset() es async.
    await sl.reset();
    sl.registerSingleton<FilesRepository>(_FakeFilesRepository());
  });

  testWidgets('muestra el estado vacío cuando la papelera no tiene nada',
      (tester) async {
    await tester.pumpWidget(const MaterialApp(home: TrashPage()));
    await tester.pumpAndSettle();

    expect(find.text('La papelera está vacía'), findsOneWidget);
  });

  testWidgets('lista archivos y carpetas con su ubicación y fecha de borrado',
      (tester) async {
    final repo = sl<FilesRepository>() as _FakeFilesRepository;
    repo.trashListing = DirectoryListing(
      directories: [
        DirectoryEntry(
          id: 'd1',
          parentPath: '/Documentos',
          name: 'Vieja',
          createdAt: DateTime.utc(2026),
          deletedAt: DateTime.utc(2026, 2, 1, 10, 30),
        ),
      ],
      files: [_trashedFile(id: 'f1', name: 'borrado.txt')],
    );

    await tester.pumpWidget(const MaterialApp(home: TrashPage()));
    await tester.pumpAndSettle();

    expect(find.text('Vieja'), findsOneWidget);
    expect(find.text('borrado.txt'), findsOneWidget);
    expect(find.textContaining('Ubicación original: /Documentos'), findsOneWidget);
    expect(find.textContaining('2026-02-01'), findsWidgets);
  });

  testWidgets('restaurar un archivo no pide confirmación y recarga',
      (tester) async {
    final repo = sl<FilesRepository>() as _FakeFilesRepository;
    repo.trashListing = DirectoryListing(
      directories: const [],
      files: [_trashedFile(id: 'f1', name: 'borrado.txt')],
    );

    await tester.pumpWidget(const MaterialApp(home: TrashPage()));
    await tester.pumpAndSettle();

    await tester.tap(find.byTooltip('Restaurar'));
    await tester.pumpAndSettle();

    expect(repo.restoredFileIds, ['f1']);
    // Recargó la papelera de verdad (el fake ya no devuelve el archivo).
    expect(find.text('borrado.txt'), findsNothing);
    expect(find.text('La papelera está vacía'), findsOneWidget);
  });

  testWidgets(
    'eliminar para siempre pide confirmación y, tras confirmar, borra con permanent=true',
    (tester) async {
      final repo = sl<FilesRepository>() as _FakeFilesRepository;
      repo.trashListing = DirectoryListing(
        directories: const [],
        files: [_trashedFile(id: 'f1', name: 'borrado.txt')],
      );

      await tester.pumpWidget(const MaterialApp(home: TrashPage()));
      await tester.pumpAndSettle();

      await tester.tap(find.byTooltip('Eliminar para siempre'));
      await tester.pumpAndSettle();

      // El diálogo de confirmación aparece; el archivo todavía no se borró.
      expect(find.text('Eliminar para siempre'), findsWidgets); // título + botón
      expect(repo.deletedFilePermanentFlags, isEmpty);

      await tester.tap(find.widgetWithText(FilledButton, 'Eliminar para siempre'));
      await tester.pumpAndSettle();

      expect(repo.deletedFilePermanentFlags, [true]);
      expect(find.text('La papelera está vacía'), findsOneWidget);
    },
  );
}
