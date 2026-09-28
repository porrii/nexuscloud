import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/di/service_locator.dart';
import 'package:nexuscloud_client/core/network/api_client.dart';
import 'package:nexuscloud_client/core/network/api_exception.dart';
import 'package:nexuscloud_client/core/network/session_expiry_notifier.dart';
import 'package:nexuscloud_client/core/storage/token_store.dart';
import 'package:nexuscloud_client/features/auth/domain/entities/app_user.dart';
import 'package:nexuscloud_client/features/auth/domain/entities/auto_login_outcome.dart';
import 'package:nexuscloud_client/features/auth/domain/entities/login_result.dart';
import 'package:nexuscloud_client/features/auth/domain/repositories/auth_repository.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_entry.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_listing.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_entry.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_version.dart';
import 'package:nexuscloud_client/features/files/domain/repositories/files_repository.dart';
import 'package:nexuscloud_client/features/files/presentation/pages/file_browser_page.dart';
import 'package:nexuscloud_client/features/update/data/update_check_service.dart';

class _FakeTokenStore implements TokenStore {
  @override
  Future<void> save(String token) async {}
  @override
  Future<String?> read() async => null;
  @override
  Future<void> clear() async {}
}

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

  final List<String> createdDirectories = [];

  @override
  Future<void> createDirectory({
    required String parentPath,
    required String name,
  }) async {
    createdDirectories.add('$parentPath|$name');
  }

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

  /// `id|nuevoPadre|nuevoNombre` de cada movimiento/renombrado pedido.
  final List<String> moves = [];

  @override
  Future<FileEntry> moveFile(String fileId, {String? newParentPath, String? newName}) async {
    moves.add('$fileId|$newParentPath|$newName');
    return FileEntry(
      id: fileId,
      parentPath: newParentPath ?? '/',
      name: newName ?? 'x',
      sizeBytes: 0,
      sha256: '',
      mimeType: '',
      createdAt: DateTime.utc(2026),
      updatedAt: DateTime.utc(2026),
    );
  }

  @override
  Future<DirectoryEntry> moveDirectory(String directoryId, {String? newParentPath, String? newName}) async {
    moves.add('$directoryId|$newParentPath|$newName');
    return DirectoryEntry(
      id: directoryId,
      parentPath: newParentPath ?? '/',
      name: newName ?? 'x',
      createdAt: DateTime.utc(2026),
    );
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

  // El historial de versiones es responsabilidad de FileVersionsPage, no
  // de FileBrowserPage -- este archivo solo comprueba que el icono
  // aparece en el sitio correcto, nunca llega a tocarlo.
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

void main() {
  setUp(() async {
    // Ver nota en login_page_test.dart: GetIt.reset() es async y hay que
    // esperarlo antes de volver a registrar, o la limpieza puede pisar el
    // registro de forma intermitente.
    await sl.reset();
    sl.registerSingleton<AuthRepository>(_FakeAuthRepository());
    sl.registerSingleton<FilesRepository>(_FakeFilesRepository());
    // FileBrowserPage lanza una comprobación de actualizaciones silenciosa
    // en segundo plano al abrirse (ver ADR-032) -- sin baseUrl configurada
    // a propósito, para que la petición falle rápido (sin red real) y
    // UpdateCheckService.check() degrade a "unavailable" sin más, que es
    // justo lo que este grupo de tests necesita: ni lo prueban ni les
    // debe importar.
    sl.registerSingleton<UpdateCheckService>(
      UpdateCheckService(
        apiClient: ApiClient(
          tokenStore: _FakeTokenStore(),
          sessionExpiryNotifier: SessionExpiryNotifier(),
        ),
        currentVersion: '0.0.0',
      ),
    );
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
      // "Sincronización" ya no es un icono del explorador: es una sección
      // de la barra lateral del shell (AppShell).
      expect(find.text('Nueva carpeta'), findsOneWidget);
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

  testWidgets(
    'el Historial de versiones solo aparece en el menú de un archivo, no de una carpeta',
    (tester) async {
      final filesRepo = sl<FilesRepository>() as _FakeFilesRepository;
      filesRepo.listingsByPath['/'] = DirectoryListing(
        directories: [
          DirectoryEntry(
            id: 'd1',
            parentPath: '/',
            name: 'Carpeta',
            createdAt: DateTime.utc(2026),
          ),
        ],
        files: [
          FileEntry(
            id: 'f1',
            parentPath: '/',
            name: 'archivo.txt',
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

      // Menú "Más acciones" de la fila del archivo: incluye el historial.
      await tester.tap(find.descendant(
        of: find.byKey(const ValueKey('entry-f:f1')),
        matching: find.byTooltip('Más acciones'),
      ));
      await tester.pumpAndSettle();
      expect(find.text('Historial de versiones'), findsOneWidget);
      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pumpAndSettle();

      // El de la carpeta, no (solo los archivos tienen versiones).
      await tester.tap(find.descendant(
        of: find.byKey(const ValueKey('entry-d:d1')),
        matching: find.byTooltip('Más acciones'),
      ));
      await tester.pumpAndSettle();
      expect(find.text('Renombrar'), findsOneWidget);
      expect(find.text('Historial de versiones'), findsNothing);
    },
  );

  testWidgets(
    'el icono de Compartir aparece tanto en filas de carpeta como de archivo',
    (tester) async {
      final filesRepo = sl<FilesRepository>() as _FakeFilesRepository;
      filesRepo.listingsByPath['/'] = DirectoryListing(
        directories: [
          DirectoryEntry(
            id: 'd1',
            parentPath: '/',
            name: 'Carpeta',
            createdAt: DateTime.utc(2026),
          ),
        ],
        files: [
          FileEntry(
            id: 'f1',
            parentPath: '/',
            name: 'archivo.txt',
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

      // A diferencia de Historial (solo archivo), Compartir aparece en
      // ambas filas -> una carpeta + un archivo = 2 iconos.
      expect(find.byTooltip('Compartir'), findsNWidgets(2));
      expect(
        find.descendant(
          of: find.byKey(const ValueKey('entry-d:d1')),
          matching: find.byTooltip('Compartir'),
        ),
        findsOneWidget,
      );
      expect(
        find.descendant(
          of: find.byKey(const ValueKey('entry-f:f1')),
          matching: find.byTooltip('Compartir'),
        ),
        findsOneWidget,
      );
    },
  );

  group('explorador de escritorio', () {
    FileEntry file(String id, String name, int size, DateTime updated) => FileEntry(
          id: id,
          parentPath: '/',
          name: name,
          sizeBytes: size,
          sha256: 'abc',
          mimeType: 'text/plain',
          createdAt: DateTime.utc(2026),
          updatedAt: updated,
        );

    void seedRoot(_FakeFilesRepository repo) {
      repo.listingsByPath['/'] = DirectoryListing(
        directories: [
          DirectoryEntry(id: 'd1', parentPath: '/', name: 'Fotos', createdAt: DateTime.utc(2026)),
        ],
        files: [
          file('f1', 'notas.txt', 10, DateTime.utc(2026, 1, 1)),
          file('f2', 'grande.iso', 5000000, DateTime.utc(2026, 1, 2)),
          file('f3', 'apuntes.md', 300, DateTime.utc(2026, 1, 3)),
        ],
      );
    }

    List<String> visibleOrder(WidgetTester tester) {
      final keys = tester
          .widgetList(find.byWidgetPredicate(
            (w) => w.key is ValueKey<String> &&
                (w.key! as ValueKey<String>).value.startsWith('entry-'),
          ))
          .map((w) => (w.key! as ValueKey<String>).value)
          .toList();
      return keys;
    }

    testWidgets('el filtro deja solo lo que coincide en la carpeta actual', (tester) async {
      final repo = sl<FilesRepository>() as _FakeFilesRepository;
      seedRoot(repo);
      await tester.pumpWidget(const MaterialApp(home: FileBrowserPage()));
      await tester.pumpAndSettle();

      await tester.enterText(find.widgetWithText(TextField, 'Filtrar en esta carpeta'), 'NOT');
      await tester.pumpAndSettle();

      expect(find.text('notas.txt'), findsOneWidget);
      expect(find.text('grande.iso'), findsNothing);
      expect(find.text('Fotos'), findsNothing);
    });

    testWidgets('las carpetas van primero y la cabecera Tamaño reordena los archivos', (tester) async {
      final repo = sl<FilesRepository>() as _FakeFilesRepository;
      seedRoot(repo);
      await tester.pumpWidget(const MaterialApp(home: FileBrowserPage()));
      await tester.pumpAndSettle();

      // Por nombre ascendente: la carpeta primero, luego a-z.
      expect(visibleOrder(tester), ['entry-d:d1', 'entry-f:f3', 'entry-f:f2', 'entry-f:f1']);

      // Tamaño empieza descendente (lo más grande primero).
      await tester.tap(find.text('TAMAÑO'));
      await tester.pumpAndSettle();
      expect(visibleOrder(tester), ['entry-d:d1', 'entry-f:f2', 'entry-f:f3', 'entry-f:f1']);
    });

    testWidgets('crea una carpeta nueva y recarga el listado', (tester) async {
      final repo = sl<FilesRepository>() as _FakeFilesRepository;
      seedRoot(repo);
      await tester.pumpWidget(const MaterialApp(home: FileBrowserPage()));
      await tester.pumpAndSettle();
      final requestsBefore = repo.requestedPaths.length;

      await tester.tap(find.widgetWithText(OutlinedButton, 'Nueva carpeta'));
      await tester.pumpAndSettle();
      await tester.enterText(find.widgetWithText(TextField, 'Nombre de la carpeta'), 'Facturas');
      await tester.tap(find.widgetWithText(FilledButton, 'Crear'));
      await tester.pumpAndSettle();

      expect(repo.createdDirectories, ['/|Facturas']);
      expect(repo.requestedPaths.length, greaterThan(requestsBefore));
    });

    testWidgets('no deja crear una carpeta con un nombre que ya existe aquí', (tester) async {
      final repo = sl<FilesRepository>() as _FakeFilesRepository;
      seedRoot(repo);
      await tester.pumpWidget(const MaterialApp(home: FileBrowserPage()));
      await tester.pumpAndSettle();

      await tester.tap(find.widgetWithText(OutlinedButton, 'Nueva carpeta'));
      await tester.pumpAndSettle();
      await tester.enterText(find.widgetWithText(TextField, 'Nombre de la carpeta'), 'fotos');
      await tester.tap(find.widgetWithText(FilledButton, 'Crear'));
      await tester.pumpAndSettle();

      expect(find.text('Ya hay un elemento con ese nombre aquí.'), findsOneWidget);
      expect(repo.createdDirectories, isEmpty);
    });

    testWidgets('renombra un archivo desde el menú contextual', (tester) async {
      final repo = sl<FilesRepository>() as _FakeFilesRepository;
      seedRoot(repo);
      await tester.pumpWidget(const MaterialApp(home: FileBrowserPage()));
      await tester.pumpAndSettle();

      await tester.tap(find.descendant(
        of: find.byKey(const ValueKey('entry-f:f1')),
        matching: find.byTooltip('Más acciones'),
      ));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Renombrar'));
      await tester.pumpAndSettle();

      await tester.enterText(find.widgetWithText(TextField, 'Nuevo nombre'), 'notas-2026.txt');
      await tester.tap(find.widgetWithText(FilledButton, 'Renombrar'));
      await tester.pumpAndSettle();

      expect(repo.moves, ['f1|null|notas-2026.txt']);
    });

    testWidgets('Ctrl+clic suma a la selección y Supr manda el lote a la papelera', (tester) async {
      final repo = sl<FilesRepository>() as _FakeFilesRepository;
      seedRoot(repo);
      await tester.pumpWidget(const MaterialApp(home: FileBrowserPage()));
      await tester.pumpAndSettle();

      await tester.tap(find.text('notas.txt'));
      await tester.pump(const Duration(milliseconds: 500));
      await tester.sendKeyDownEvent(LogicalKeyboardKey.controlLeft);
      await tester.tap(find.text('apuntes.md'));
      await tester.sendKeyUpEvent(LogicalKeyboardKey.controlLeft);
      await tester.pumpAndSettle();

      expect(find.text('2 seleccionados'), findsOneWidget);

      await tester.sendKeyEvent(LogicalKeyboardKey.delete);
      await tester.pumpAndSettle();
      expect(find.text('Mover 2 elementos a la papelera'), findsOneWidget);

      await tester.tap(find.widgetWithText(FilledButton, 'Mover a la papelera'));
      await tester.pumpAndSettle();

      expect(repo.deletedFileIds, unorderedEquals(['f1', 'f3']));
    });

    testWidgets('un clic en un archivo solo lo selecciona; Inicio + Intro entra en la carpeta', (tester) async {
      final repo = sl<FilesRepository>() as _FakeFilesRepository;
      seedRoot(repo);
      repo.listingsByPath['/Fotos'] = const DirectoryListing(directories: [], files: []);
      await tester.pumpWidget(const MaterialApp(home: FileBrowserPage()));
      await tester.pumpAndSettle();

      // Un clic en un archivo solo lo selecciona.
      await tester.tap(find.text('notas.txt'));
      await tester.pumpAndSettle();
      expect(find.text('1 seleccionado'), findsOneWidget);
      expect(repo.requestedPaths, isNot(contains('/Fotos')));

      // Inicio lleva la selección a la primera fila (la carpeta) e Intro
      // la abre.
      await tester.sendKeyEvent(LogicalKeyboardKey.home);
      await tester.sendKeyEvent(LogicalKeyboardKey.enter);
      await tester.pumpAndSettle();
      expect(repo.requestedPaths, contains('/Fotos'));
    });
  });
}
