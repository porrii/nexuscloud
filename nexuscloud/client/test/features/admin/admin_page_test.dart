import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/features/admin/data/admin_models.dart';
import 'package:nexuscloud_client/features/admin/data/admin_service.dart';
import 'package:nexuscloud_client/features/admin/presentation/pages/admin_page.dart';
import 'package:nexuscloud_client/features/shell/presentation/widgets/sidebar.dart';

/// Servicio falso en memoria: solo lo que ejercitan estos tests.
class _FakeAdmin implements AdminService {
  final users = <AdminUser>[
    AdminUser(
      id: 'me',
      username: 'admin',
      displayName: 'Admin',
      active: true,
      hasTotp: true,
      createdAt: DateTime(2026),
    ),
    AdminUser(
      id: 'u2',
      username: 'maria',
      displayName: 'María',
      active: true,
      hasTotp: false,
      createdAt: DateTime(2026),
      quotaBytes: 0,
    ),
  ];
  final created = <String>[];
  final deleted = <String>[];
  final updates = <String>[];
  final groups = <AdminGroup>[];

  @override
  Future<bool?> isAdmin() async => true;

  @override
  Future<List<AdminUser>> listUsers() async => List.of(users);

  @override
  Future<AdminUser> createUser({
    required String username,
    required String password,
    required UserRole role,
    String? displayName,
    String? email,
    QuotaBytes quotaBytes,
    bool setQuota = false,
  }) async {
    created.add('$username|${role.id}|$quotaBytes|$setQuota');
    return AdminUser(
      id: 'new',
      username: username,
      displayName: displayName ?? '',
      active: true,
      hasTotp: false,
      createdAt: DateTime(2026),
    );
  }

  @override
  Future<AdminUser> updateUser(
    String id, {
    String? displayName,
    String? email,
    bool? active,
    QuotaBytes quotaBytes,
    bool setQuota = false,
  }) async {
    updates.add('$id|$active');
    return users.firstWhere((u) => u.id == id);
  }

  @override
  Future<void> deleteUser(String id) async => deleted.add(id);

  @override
  Future<List<AdminGroup>> listGroups() async => List.of(groups);

  @override
  Future<AdminGroup> createGroup(String name, {QuotaBytes quotaBytes}) async {
    final group = AdminGroup(id: 'g${groups.length}', name: name, quotaBytes: quotaBytes);
    groups.add(group);
    return group;
  }

  @override
  Future<List<AuditEvent>> listAuditEvents({int limit = 100, int offset = 0}) async => [
        AuditEvent(
          id: 'e1',
          occurredAt: DateTime.now(),
          eventType: 'login_failed',
          actorUserId: 'u2',
          ip: '10.0.0.5',
        ),
      ];

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

void main() {
  late _FakeAdmin admin;

  setUp(() => admin = _FakeAdmin());

  Future<void> pumpAdmin(WidgetTester tester) async {
    tester.view.physicalSize = const Size(1400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(MaterialApp(
      home: AdminPage(currentUserId: 'me', service: admin),
    ));
    await tester.pumpAndSettle();
  }

  Future<void> openMenu(WidgetTester tester, String userId) async {
    await tester.tap(find.descendant(
      of: find.byKey(ValueKey('user-$userId')),
      matching: find.byTooltip('Más acciones'),
    ));
    await tester.pumpAndSettle();
  }

  testWidgets('lista los usuarios y no deja desactivar ni borrar tu cuenta', (tester) async {
    await pumpAdmin(tester);

    expect(find.text('María'), findsOneWidget);
    expect(find.text('Tú'), findsOneWidget);

    await openMenu(tester, 'me');
    expect(find.text('Editar'), findsOneWidget);
    expect(find.text('Desactivar'), findsNothing);
    expect(find.text('Eliminar'), findsNothing);
    await tester.tapAt(const Offset(5, 5));
    await tester.pumpAndSettle();

    await openMenu(tester, 'u2');
    expect(find.text('Desactivar'), findsOneWidget);
    expect(find.text('Eliminar'), findsOneWidget);
  });

  testWidgets('crear usuario exige usuario y contraseña de 8+ y envía el rol', (tester) async {
    await pumpAdmin(tester);
    await tester.tap(find.widgetWithText(FilledButton, 'Nuevo usuario'));
    await tester.pumpAndSettle();

    final create = find.widgetWithText(FilledButton, 'Crear usuario');
    await tester.enterText(find.widgetWithText(TextField, 'Nombre de usuario'), 'luis');
    await tester.enterText(
      find.widgetWithText(TextField, 'Contraseña (mínimo 8 caracteres)'),
      'corta',
    );
    await tester.pump();
    expect(tester.widget<FilledButton>(create).onPressed, isNull);

    await tester.enterText(
      find.widgetWithText(TextField, 'Contraseña (mínimo 8 caracteres)'),
      'bastante-larga',
    );
    await tester.pump();
    await tester.tap(create);
    await tester.pumpAndSettle();

    expect(admin.created, ['luis|user|null|false']);
  });

  testWidgets('no ofrece el rol «Solo lectura» (el servidor no lo aplica)', (tester) async {
    await pumpAdmin(tester);
    await tester.tap(find.widgetWithText(FilledButton, 'Nuevo usuario'));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Usuario').last);
    await tester.pumpAndSettle();

    expect(find.text('Administrador'), findsWidgets);
    expect(find.text('Solo lectura'), findsNothing);
  });

  testWidgets('generar contraseña no la copia; «Copiar» sí, y se borra al minuto', (tester) async {
    final clipboard = <String?>[];
    tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
      SystemChannels.platform,
      (call) async {
        if (call.method == 'Clipboard.setData') {
          clipboard.add((call.arguments as Map)['text'] as String?);
        }
        if (call.method == 'Clipboard.getData') {
          return {'text': clipboard.isEmpty ? null : clipboard.last};
        }
        return null;
      },
    );
    addTearDown(() => tester.binding.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, null));

    await pumpAdmin(tester);
    await tester.tap(find.widgetWithText(FilledButton, 'Nuevo usuario'));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('Generar una contraseña'));
    await tester.pump();
    expect(clipboard, isEmpty);

    await tester.tap(find.byTooltip('Copiar'));
    await tester.pump();
    expect(clipboard, hasLength(1));
    expect(clipboard.single!.length, 16);

    await tester.pump(const Duration(seconds: 61));
    expect(clipboard.last, '');
  });

  testWidgets('eliminar exige escribir el nombre de usuario', (tester) async {
    await pumpAdmin(tester);
    await openMenu(tester, 'u2');
    await tester.tap(find.text('Eliminar'));
    await tester.pumpAndSettle();

    final confirm = find.widgetWithText(FilledButton, 'Eliminar para siempre');
    expect(tester.widget<FilledButton>(confirm).onPressed, isNull);

    await tester.enterText(find.byType(TextField).last, 'maria');
    await tester.pump();
    await tester.tap(confirm);
    await tester.pumpAndSettle();

    expect(admin.deleted, ['u2']);
  });

  testWidgets('crea un grupo desde su pestaña', (tester) async {
    await pumpAdmin(tester);
    await tester.tap(find.text('Grupos'));
    await tester.pumpAndSettle();
    expect(find.text('Todavía no hay grupos'), findsOneWidget);

    await tester.tap(find.widgetWithText(FilledButton, 'Nuevo grupo'));
    await tester.pumpAndSettle();
    await tester.enterText(find.widgetWithText(TextField, 'Nombre'), 'Familia');
    await tester.pump();
    await tester.tap(find.widgetWithText(FilledButton, 'Crear grupo'));
    await tester.pumpAndSettle();

    expect(find.text('Familia'), findsOneWidget);
  });

  testWidgets('la auditoría traduce el tipo y el actor', (tester) async {
    await pumpAdmin(tester);
    await tester.tap(find.text('Auditoría'));
    await tester.pumpAndSettle();

    expect(find.text('Inicio de sesión fallido'), findsOneWidget);
    expect(find.textContaining('maria · 10.0.0.5'), findsOneWidget);
  });

  testWidgets('la barra lateral solo muestra Administración a administradores', (tester) async {
    Widget sidebar({required bool isAdmin}) => MaterialApp(
          home: Scaffold(
            body: Sidebar(
              current: ShellSection.files,
              onSelect: (_) {},
              onSearch: (_) {},
              searchController: TextEditingController(),
              searchFocus: FocusNode(),
              syncActivity: null,
              quota: null,
              onRefreshQuota: () {},
              user: null,
              updateAvailable: false,
              onLogout: () {},
              isAdmin: isAdmin,
            ),
          ),
        );

    await tester.pumpWidget(sidebar(isAdmin: false));
    expect(find.text('Administración'), findsNothing);

    await tester.pumpWidget(sidebar(isAdmin: true));
    expect(find.text('Administración'), findsOneWidget);
  });
}
