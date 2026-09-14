import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/network/api_client.dart';
import 'package:nexuscloud_client/core/network/api_exception.dart';
import 'package:nexuscloud_client/core/network/retry_policy.dart';
import 'package:nexuscloud_client/core/network/session_expiry_notifier.dart';
import 'package:nexuscloud_client/core/storage/token_store.dart';

/// Fake hand-written (sin mocktail/mockito, mismo criterio que NexusKeys).
class _FakeTokenStore implements TokenStore {
  String? token;

  @override
  Future<void> save(String token) async => this.token = token;

  @override
  Future<String?> read() async => token;

  @override
  Future<void> clear() async => token = null;
}

class _FakeResponse {
  _FakeResponse(this.statusCode, this.body)
      : isConnectionError = false,
        failMidStreamAfterBytes = null;
  _FakeResponse.connectionError()
      : statusCode = -1,
        body = const {},
        isConnectionError = true,
        failMidStreamAfterBytes = null;

  /// Simula una conexión que responde 200 pero se corta a mitad de la
  /// transmisión -- para probar que `dio.download` no deja un archivo a
  /// medias (ADR-010), a diferencia de un error limpio antes de empezar a
  /// escribir a disco.
  _FakeResponse.streamFailsMidway()
      : statusCode = 200,
        body = const {},
        isConnectionError = false,
        failMidStreamAfterBytes = 3;

  final int statusCode;
  final Map<String, dynamic> body;
  final bool isConnectionError;
  final int? failMidStreamAfterBytes;
}

/// Adaptador HTTP falso -- responde según una cola programada por el test,
/// sin abrir ningún socket real.
class _FakeHttpClientAdapter implements HttpClientAdapter {
  final List<_FakeResponse> queue = [];
  final List<RequestOptions> requests = [];

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    requests.add(options);
    if (queue.isEmpty) {
      throw StateError('Sin respuesta programada para ${options.path}');
    }
    final next = queue.removeAt(0);
    if (next.isConnectionError) {
      throw Exception('conexión rechazada (simulada)');
    }
    if (next.failMidStreamAfterBytes != null) {
      return ResponseBody(
        _flakyStream(next.failMidStreamAfterBytes!),
        next.statusCode,
      );
    }
    return ResponseBody.fromString(
      jsonEncode(next.body),
      next.statusCode,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );
  }

  Stream<Uint8List> _flakyStream(int bytesBeforeFailure) async* {
    yield Uint8List.fromList(List.filled(bytesBeforeFailure, 1));
    throw Exception('conexión perdida a mitad de la transmisión (simulada)');
  }

  @override
  void close({bool force = false}) {}
}

void main() {
  late _FakeHttpClientAdapter adapter;
  late _FakeTokenStore tokenStore;
  late SessionExpiryNotifier sessionExpiryNotifier;
  late ApiClient apiClient;

  setUp(() {
    adapter = _FakeHttpClientAdapter();
    tokenStore = _FakeTokenStore();
    sessionExpiryNotifier = SessionExpiryNotifier();
    final dio = Dio()..httpClientAdapter = adapter;
    apiClient = ApiClient(
      tokenStore: tokenStore,
      sessionExpiryNotifier: sessionExpiryNotifier,
      retryPolicy: RetryPolicy(delayFn: (_) async {}),
      dio: dio,
    );
    apiClient.configureBaseUrl('http://test.local');
  });

  tearDown(() => sessionExpiryNotifier.dispose());

  test('401 con Authorization dispara SessionExpiryNotifier', () async {
    tokenStore.token = 'a-token';
    adapter.queue.add(_FakeResponse(401, {
      'error': {'code': 'unauthorized', 'message': 'Sesión inválida.'},
    }));

    final expiredFuture = sessionExpiryNotifier.onSessionExpired.first;

    await expectLater(
      apiClient.request((d) => d.get<Map<String, dynamic>>('/files')),
      throwsA(isA<ApiException>().having((e) => e.code, 'code', 'unauthorized')),
    );

    await expectLater(expiredFuture, completes);
  });

  test('401 sin Authorization (login) NO dispara SessionExpiryNotifier', () async {
    adapter.queue.add(_FakeResponse(401, {
      'error': {
        'code': 'unauthorized',
        'message': 'Usuario o contraseña incorrectos.',
      },
    }));

    var expired = false;
    sessionExpiryNotifier.onSessionExpired.listen((_) => expired = true);

    await expectLater(
      apiClient.request((d) => d.post<Map<String, dynamic>>('/auth/login')),
      throwsA(isA<ApiException>()),
    );

    expect(expired, isFalse);
  });

  test('400 invalid_request se traduce en ApiException con ese código', () async {
    adapter.queue.add(_FakeResponse(400, {
      'error': {'code': 'invalid_request', 'message': 'Falta un campo.'},
    }));

    await expectLater(
      apiClient.request((d) => d.post<Map<String, dynamic>>('/directories')),
      throwsA(
        isA<ApiException>().having((e) => e.code, 'code', 'invalid_request'),
      ),
    );
  });

  test('un error de conexión se traduce en ApiException.network', () async {
    adapter.queue.add(_FakeResponse.connectionError());

    await expectLater(
      apiClient.request((d) => d.get<Map<String, dynamic>>('/files')),
      throwsA(
        isA<ApiException>().having((e) => e.code, 'code', 'network_error'),
      ),
    );
  });

  test('429 se reintenta hasta agotar el máximo y propaga rate_limited', () async {
    for (var i = 0; i < 4; i++) {
      adapter.queue.add(_FakeResponse(429, {
        'error': {'code': 'rate_limited', 'message': 'Demasiadas peticiones.'},
      }));
    }

    await expectLater(
      apiClient.request((d) => d.get<Map<String, dynamic>>('/files')),
      throwsA(
        isA<ApiException>().having((e) => e.code, 'code', 'rate_limited'),
      ),
    );

    // 1 intento original + 3 reintentos (RetryPolicy.maxAttempts por defecto).
    expect(adapter.requests.length, 4);
  });

  test('429 seguido de éxito resuelve con la respuesta buena', () async {
    adapter.queue
      ..add(_FakeResponse(429, {
        'error': {'code': 'rate_limited', 'message': 'x'},
      }))
      ..add(_FakeResponse(200, {'directories': [], 'files': []}));

    final response = await apiClient.request(
      (d) => d.get<Map<String, dynamic>>('/files'),
    );

    expect(response.data, {'directories': [], 'files': []});
    expect(adapter.requests.length, 2);
  });

  test('el token guardado se adjunta como Authorization Bearer', () async {
    tokenStore.token = 'mi-token-secreto';
    adapter.queue.add(_FakeResponse(200, {'directories': [], 'files': []}));

    await apiClient.request((d) => d.get<Map<String, dynamic>>('/files'));

    expect(
      adapter.requests.single.headers['Authorization'],
      'Bearer mi-token-secreto',
    );
  });

  test(
    'un cuerpo Stream (subida) no se reintenta automáticamente en 429',
    () async {
      adapter.queue.add(_FakeResponse(429, {
        'error': {'code': 'rate_limited', 'message': 'Demasiadas peticiones.'},
      }));

      final controller = StreamController<List<int>>();
      controller.add([1, 2, 3]);
      unawaited(controller.close());

      await expectLater(
        apiClient.request(
          (d) => d.post<Map<String, dynamic>>(
            '/files',
            data: controller.stream,
            options: Options(headers: {Headers.contentLengthHeader: 3}),
          ),
        ),
        throwsA(
          isA<ApiException>().having((e) => e.code, 'code', 'rate_limited'),
        ),
      );

      // Sin reintento: si lo hubiera intentado, habría lanzado StateError
      // (stream de una sola suscripción ya consumido) en vez de propagar
      // rate_limited con limpieza -- y el adaptador habría visto 2+
      // peticiones en vez de 1.
      expect(adapter.requests.length, 1);
    },
  );

  test(
    'una descarga que se corta a medias no deja el archivo en disco',
    () async {
      final tempDir = Directory.systemTemp.createTempSync('nexuscloud_dl_');
      addTearDown(() {
        if (tempDir.existsSync()) tempDir.deleteSync(recursive: true);
      });
      final savePath = '${tempDir.path}/archivo.bin';

      adapter.queue.add(_FakeResponse.streamFailsMidway());

      await expectLater(
        apiClient.request((d) => d.download('/files/f1', savePath)),
        throwsA(anything),
      );

      // dio.download usa deleteOnError:true por defecto -- no hay que
      // borrar el archivo a mano en el data source (ADR-010); esta prueba
      // guarda contra que ese valor por defecto cambie algún día.
      expect(File(savePath).existsSync(), isFalse);
    },
  );
}
