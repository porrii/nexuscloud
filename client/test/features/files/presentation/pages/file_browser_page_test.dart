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
}
