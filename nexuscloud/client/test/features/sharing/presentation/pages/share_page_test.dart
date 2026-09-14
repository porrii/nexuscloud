import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/di/service_locator.dart';
import 'package:nexuscloud_client/core/network/api_exception.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_listing.dart';
import 'package:nexuscloud_client/features/files/domain/repositories/files_repository.dart' show TransferProgress;
import 'package:nexuscloud_client/features/sharing/domain/entities/group.dart';
import 'package:nexuscloud_client/features/sharing/domain/entities/share.dart';
import 'package:nexuscloud_client/features/sharing/domain/repositories/sharing_repository.dart';
import 'package:nexuscloud_client/features/sharing/presentation/pages/share_page.dart';

class _FakeSharingRepository implements SharingRepository {
  List<Group> groups = const [];
  List<Share> allShares = [];
  ApiException? loadError;

  Share? createResult;
  ApiException? createError;

  final List<String> revokedIds = [];
  String? serverUrl = 'http://localhost:8080';

  @override
  Future<List<Group>> listGroups() async {
    if (loadError != null) throw loadError!;
    return groups;
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
    if (createError != null) throw createError!;
    final created = createResult!;
    // Tras crear, la nueva compartición aparece en `listShares` -- igual
    // criterio que `trash_page_test.dart`: mutar lo que la siguiente
    // llamada devuelve, para comprobar la recarga de verdad.
    allShares = [...allShares, created];
    return created;
  }

  @override
  Future<List<Share>> listShares({required ShareDirection direction}) async {
    if (loadError != null) throw loadError!;
    return allShares;
  }

  @override
  Future<void> revokeShare(String shareId) async {
    revokedIds.add(shareId);
    allShares = allShares.where((s) => s.id != shareId).toList();
  }

  @override
  Future<String?> get serverBaseUrl => Future.value(serverUrl);

  // Navegar/descargar comparticiones recibidas es responsabilidad de
  // SharedWithMePage, no de SharePage -- no los ejercita ningún test de
  // este archivo.
  @override
  Future<DirectoryListing> listSharedDirectory(String directoryId) =>
      throw UnimplementedError();

  @override
  Future<void> downloadSharedFile({
    required String fileId,
    required String saveToPath,
    TransferProgress? onProgress,
  }) =>
      throw UnimplementedError();
}

Share _userShare({
  String id = 's1',
  String resourceId = 'f1',
  ShareResourceType resourceType = ShareResourceType.file,
  String targetUsername = 'alice',
}) =>
    Share(
      id: id,
      resourceType: resourceType,
      resourceId: resourceId,
      shareType: ShareType.user,
      targetUsername: targetUsername,
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

    // `Clipboard.setData` pasa por un canal de plataforma real -- sin este
    // mock lanzaría `MissingPluginException` en el test, igual que
    // `getSaveLocation()` sin mockear.
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, (call) async {
      return null;
    });
  });

  testWidgets('muestra el estado vacío cuando no hay comparticiones activas',
      (tester) async {
    await tester.pumpWidget(const MaterialApp(
      home: SharePage(
        resourceId: 'f1',
        resourceName: 'informe.pdf',
        resourceType: ShareResourceType.file,
      ),
    ));
    await tester.pumpAndSettle();

    expect(find.text('Nadie tiene acceso todavía.'), findsOneWidget);
  });

  testWidgets('lista comparticiones activas con su objetivo y metadatos',
      (tester) async {
    final repo = sl<SharingRepository>() as _FakeSharingRepository;
    repo.allShares = [_userShare()];

    await tester.pumpWidget(const MaterialApp(
      home: SharePage(
        resourceId: 'f1',
        resourceName: 'informe.pdf',
        resourceType: ShareResourceType.file,
      ),
    ));
    await tester.pumpAndSettle();

    expect(find.text('Usuario: alice'), findsOneWidget);
    expect(find.text('Solo descarga'), findsOneWidget);
  });

  testWidgets(
    'el checkbox "Permitir subir" solo aparece cuando el recurso es una carpeta',
    (tester) async {
      // Archivo: sin checkbox.
      await tester.pumpWidget(const MaterialApp(
        home: SharePage(
          resourceId: 'f1',
          resourceName: 'informe.pdf',
          resourceType: ShareResourceType.file,
        ),
      ));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Enlace'));
      await tester.pumpAndSettle();
      expect(
        find.text('Permitir subir archivos a esta carpeta'),
        findsNothing,
      );

      // Carpeta: sí aparece.
      await tester.pumpWidget(const MaterialApp(
        home: SharePage(
          resourceId: 'd1',
          resourceName: 'Documentos',
          resourceType: ShareResourceType.directory,
        ),
      ));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Enlace'));
      await tester.pumpAndSettle();
      expect(
        find.text('Permitir subir archivos a esta carpeta'),
        findsOneWidget,
      );
    },
  );

  testWidgets(
    'crear una compartición de usuario recarga la lista de comparticiones activas',
    (tester) async {
      final repo = sl<SharingRepository>() as _FakeSharingRepository;
      repo.createResult = _userShare();

      await tester.pumpWidget(const MaterialApp(
        home: SharePage(
          resourceId: 'f1',
          resourceName: 'informe.pdf',
          resourceType: ShareResourceType.file,
        ),
      ));
      await tester.pumpAndSettle();

      expect(find.text('Nadie tiene acceso todavía.'), findsOneWidget);

      await tester.enterText(
        find.widgetWithText(TextFormField, 'Nombre de usuario'),
        'alice',
      );
      await tester.tap(find.widgetWithText(FilledButton, 'Compartir'));
      await tester.pumpAndSettle();

      expect(find.text('Usuario: alice'), findsOneWidget);
      expect(find.text('Nadie tiene acceso todavía.'), findsNothing);
    },
  );

  testWidgets(
    'crear un enlace muestra el bloque "Enlace creado" con la URL construida a partir del token',
    (tester) async {
      // La página (comparticiones activas + los 3 formularios + el bloque
      // de confirmación) es más alta que el viewport de test por defecto
      // (800x600) -- `ensureVisible` no basta para llevar "Copiar" a una
      // posición donde `tap()` de verdad impacte el widget (queda fuera de
      // los límites físicos de la superficie de renderizado, no solo
      // desplazado dentro de un scroll). Se agranda la superficie para
      // esta prueba en vez de perseguir el scroll.
      tester.view.physicalSize = const Size(800, 2400);
      tester.view.devicePixelRatio = 1.0;
      addTearDown(tester.view.reset);

      final repo = sl<SharingRepository>() as _FakeSharingRepository
        ..serverUrl = 'http://localhost:8080';
      repo.createResult = Share(
        id: 's1',
        resourceType: ShareResourceType.file,
        resourceId: 'f1',
        shareType: ShareType.link,
        canDownload: true,
        canUpload: false,
        hasPassword: false,
        downloadCount: 0,
        createdAt: DateTime.utc(2026),
        token: 'tok_abc123',
      );

      await tester.pumpWidget(const MaterialApp(
        home: SharePage(
          resourceId: 'f1',
          resourceName: 'informe.pdf',
          resourceType: ShareResourceType.file,
        ),
      ));
      await tester.pumpAndSettle();

      await tester.tap(find.text('Enlace'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'Crear enlace'));
      await tester.pumpAndSettle();

      expect(find.textContaining('Enlace creado'), findsOneWidget);
      expect(find.text('http://localhost:8080/s/tok_abc123'), findsOneWidget);

      // No debe lanzar MissingPluginException gracias al mock de setUp().
      await tester.tap(find.text('Copiar'));
      await tester.pumpAndSettle();
    },
  );

  testWidgets(
    'un error al crear se muestra inline sin vaciar los campos ya escritos',
    (tester) async {
      (sl<SharingRepository>() as _FakeSharingRepository).createError =
          const ApiException(
        code: 'invalid_request',
        message: 'El usuario destino no existe.',
      );

      await tester.pumpWidget(const MaterialApp(
        home: SharePage(
          resourceId: 'f1',
          resourceName: 'informe.pdf',
          resourceType: ShareResourceType.file,
        ),
      ));
      await tester.pumpAndSettle();

      await tester.enterText(
        find.widgetWithText(TextFormField, 'Nombre de usuario'),
        'no-existe',
      );
      await tester.tap(find.widgetWithText(FilledButton, 'Compartir'));
      await tester.pumpAndSettle();

      expect(find.text('El usuario destino no existe.'), findsOneWidget);
      // El campo sigue mostrando lo escrito -- la página no se vació.
      expect(find.text('no-existe'), findsOneWidget);
    },
  );

  testWidgets('revocar pide confirmación y, tras confirmar, recarga la lista',
      (tester) async {
    final repo = sl<SharingRepository>() as _FakeSharingRepository;
    repo.allShares = [_userShare()];

    await tester.pumpWidget(const MaterialApp(
      home: SharePage(
        resourceId: 'f1',
        resourceName: 'informe.pdf',
        resourceType: ShareResourceType.file,
      ),
    ));
    await tester.pumpAndSettle();

    expect(find.text('Usuario: alice'), findsOneWidget);

    await tester.tap(find.text('Revocar'));
    await tester.pumpAndSettle();

    // El diálogo de confirmación aparece; todavía no se ha revocado nada.
    expect(find.text('Revocar compartición'), findsWidgets); // título + botón
    expect(repo.revokedIds, isEmpty);

    await tester.tap(find.widgetWithText(FilledButton, 'Revocar'));
    await tester.pumpAndSettle();

    expect(repo.revokedIds, ['s1']);
    expect(find.text('Usuario: alice'), findsNothing);
    expect(find.text('Nadie tiene acceso todavía.'), findsOneWidget);
  });
}
