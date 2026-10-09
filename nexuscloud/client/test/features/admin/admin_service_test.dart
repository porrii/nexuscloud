import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/network/api_client.dart';
import 'package:nexuscloud_client/core/network/session_expiry_notifier.dart';
import 'package:nexuscloud_client/core/storage/token_store.dart';
import 'package:nexuscloud_client/features/admin/data/admin_models.dart';
import 'package:nexuscloud_client/features/admin/data/admin_service.dart';

class _FakeTokenStore implements TokenStore {
  @override
  Future<void> save(String token) async {}
  @override
  Future<String?> read() async => 'token';
  @override
  Future<void> clear() async {}
}

/// Responde [body] con [status] y guarda método, ruta y cuerpo enviados.
class _Adapter implements HttpClientAdapter {
  Object? body;
  int status = 200;
  final List<(String, String, Object?)> requests = [];

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    requests.add((options.method, options.uri.toString(), options.data));
    return ResponseBody.fromString(jsonEncode(body), status, headers: {
      Headers.contentTypeHeader: [Headers.jsonContentType],
    });
  }

  @override
  void close({bool force = false}) {}
}

void main() {
  late _Adapter adapter;
  late AdminService service;

  setUp(() {
    adapter = _Adapter();
    final api = ApiClient(
      tokenStore: _FakeTokenStore(),
      sessionExpiryNotifier: SessionExpiryNotifier(),
      dio: Dio()..httpClientAdapter = adapter,
    )..configureBaseUrl('http://srv');
    service = AdminService(apiClient: api);
  });

  test('isAdmin lee is_admin de /users/me y es null («no se sabe») ante un error', () async {
    adapter.body = {'id': 'u1', 'username': 'ana', 'is_admin': true};
    expect(await service.isAdmin(), isTrue);

    adapter
      ..status = 500
      ..body = {
        'error': {'code': 'internal_error', 'message': 'x'},
      };
    expect(await service.isAdmin(), isNull);
  });

  test('updateUser distingue «no tocar la cuota» de «heredar» (null)', () async {
    adapter.body = {'id': 'u1', 'username': 'ana', 'status': 'active'};

    await service.updateUser('u1', displayName: 'Ana');
    await service.updateUser('u1', quotaBytes: null, setQuota: true);
    await service.updateUser('u1', active: false);

    final bodies = adapter.requests.map((r) => r.$3 as Map).toList();
    expect(bodies[0], {'display_name': 'Ana'});
    expect(bodies[1].containsKey('quota_bytes'), isTrue);
    expect(bodies[1]['quota_bytes'], isNull);
    expect(bodies[2], {'status': 'disabled'});
    expect(adapter.requests.first.$1, 'PATCH');
    expect(adapter.requests.first.$2, 'http://srv/api/v1/users/u1');
  });

  test('createUser envía el rol y solo la cuota si se eligió', () async {
    adapter
      ..status = 201
      ..body = {'id': 'u2', 'username': 'luis', 'status': 'active'};

    await service.createUser(
      username: 'luis',
      password: 'secreto-largo',
      role: UserRole.readOnly,
    );

    final sent = adapter.requests.single.$3 as Map;
    expect(sent['role'], 'read_only');
    expect(sent.containsKey('quota_bytes'), isFalse);
  });

  test('los eventos de auditoría usan las claves de Go (ID, EventType...)', () async {
    adapter.body = [
      {
        'ID': 'e1',
        'OccurredAt': '2026-09-28T10:00:00Z',
        'ActorUserID': 'u1',
        'EventType': 'login_failed',
        'TargetType': '',
        'TargetID': '',
        'IP': '10.0.0.5',
        'Metadata': {'username': 'ana'},
      },
    ];

    final events = await service.listAuditEvents();

    expect(events.single.eventType, 'login_failed');
    expect(events.single.actorUserId, 'u1');
    expect(events.single.targetType, isNull);
    expect(events.single.ip, '10.0.0.5');
    expect(events.single.metadata['username'], 'ana');
  });

  test('listDisks devuelve null si el servidor no sabe enumerarlos', () async {
    adapter
      ..status = 501
      ..body = {
        'error': {'code': 'not_supported', 'message': 'No soportado.'},
      };

    expect(await service.listDisks(), isNull);
  });
}
