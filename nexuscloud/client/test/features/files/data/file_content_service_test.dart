import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/network/api_client.dart';
import 'package:nexuscloud_client/core/network/api_exception.dart';
import 'package:nexuscloud_client/core/network/session_expiry_notifier.dart';
import 'package:nexuscloud_client/core/storage/token_store.dart';
import 'package:nexuscloud_client/features/files/data/file_content_service.dart';

class _FakeTokenStore implements TokenStore {
  @override
  Future<void> save(String token) async {}
  @override
  Future<String?> read() async => 'token';
  @override
  Future<void> clear() async {}
}

/// Responde siempre con [status] y el cuerpo JSON [body].
class _ErrorAdapter implements HttpClientAdapter {
  _ErrorAdapter({required this.status, required this.body});

  final int status;
  final String body;

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async => ResponseBody.fromString(body, status, headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      });

  @override
  void close({bool force = false}) {}
}

FileContentService _service(HttpClientAdapter adapter) => FileContentService(
      apiClient: ApiClient(
        tokenStore: _FakeTokenStore(),
        sessionExpiryNotifier: SessionExpiryNotifier(),
        dio: Dio()..httpClientAdapter = adapter,
      )..configureBaseUrl('http://srv'),
    );

void main() {
  test('fetchPrefix lee solo el principio y corta la conexión', () async {
    // Servidor de socket crudo (el HttpServer de dart:io acepta escrituras
    // tras el cierre del cliente y falsearía la medida): intenta mandar
    // 50 MB y cuenta cuánto llega a escribir antes de que el cliente corte.
    final server = await ServerSocket.bind(InternetAddress.loopbackIPv4, 0);
    addTearDown(server.close);
    var written = 0;
    String? request;
    final done = Completer<void>();
    server.listen((socket) async {
      socket.listen(
        (data) => request ??= utf8.decode(data, allowMalformed: true),
        onError: (_) {},
      );
      socket.add(utf8.encode(
        'HTTP/1.1 200 OK\r\nContent-Length: ${50 * 1024 * 1024}\r\n\r\n',
      ));
      try {
        for (var i = 0; i < 800; i++) {
          socket.add(Uint8List(64 * 1024));
          await socket.flush();
          written += 64 * 1024;
          await Future<void>.delayed(const Duration(milliseconds: 2));
        }
      } catch (_) {
        // El cliente cerró la conexión: es lo esperado.
      } finally {
        socket.destroy();
        done.complete();
      }
    });

    final service = FileContentService(
      apiClient: ApiClient(
        tokenStore: _FakeTokenStore(),
        sessionExpiryNotifier: SessionExpiryNotifier(),
      )..configureBaseUrl('http://127.0.0.1:${server.port}'),
    );
    final prefix = await service.fetchPrefix('v', length: 1024);
    await done.future.timeout(const Duration(seconds: 10));

    expect(prefix.length, 1024);
    expect(written, lessThan(8 * 1024 * 1024));
    // Sin Range cerrado: el servidor solo entiende «bytes=N-».
    expect(request, isNot(contains('Range:')));
  });

  test('un error JSON en una petición de bytes conserva el mensaje del servidor', () async {
    final adapter = _ErrorAdapter(
      status: 404,
      body: jsonEncode({
        'error': {'code': 'not_found', 'message': 'Archivo no encontrado.'},
      }),
    );

    await expectLater(
      _service(adapter).fetchBytes('x'),
      throwsA(
        isA<ApiException>()
            .having((e) => e.code, 'code', 'not_found')
            .having((e) => e.message, 'message', 'Archivo no encontrado.')
            .having((e) => e.statusCode, 'statusCode', 404),
      ),
    );
  });
}
