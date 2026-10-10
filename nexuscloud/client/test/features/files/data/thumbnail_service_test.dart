import 'dart:async';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/network/api_client.dart';
import 'package:nexuscloud_client/core/network/retry_policy.dart';
import 'package:nexuscloud_client/core/network/session_expiry_notifier.dart';
import 'package:nexuscloud_client/core/storage/token_store.dart';
import 'package:nexuscloud_client/features/files/data/thumbnail_service.dart';

class _FakeTokenStore implements TokenStore {
  @override
  Future<void> save(String token) async {}
  @override
  Future<String?> read() async => 'token';
  @override
  Future<void> clear() async {}
}

/// Adaptador HTTP falso: responde con [statusFor] y, si [gate] no es null,
/// retiene cada respuesta hasta que el test lo complete.
class _ThumbAdapter implements HttpClientAdapter {
  final List<String> requests = [];
  int Function(String path) statusFor = (_) => 200;
  Uint8List Function(String path) bodyFor = (_) =>
      Uint8List.fromList([1, 2, 3]);
  Completer<void>? gate;
  int inFlight = 0;
  int maxInFlight = 0;

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    requests.add(options.path);
    inFlight++;
    if (inFlight > maxInFlight) maxInFlight = inFlight;
    try {
      if (gate != null) await gate!.future;
      final status = statusFor(options.path);
      if (status != 200) {
        return ResponseBody.fromString(
          '{"error":{"code":"x","message":"x"}}',
          status,
          headers: {
            Headers.contentTypeHeader: [Headers.jsonContentType],
          },
        );
      }
      return ResponseBody.fromBytes(bodyFor(options.path), 200);
    } finally {
      inFlight--;
    }
  }

  @override
  void close({bool force = false}) {}
}

void main() {
  late _ThumbAdapter adapter;
  late ApiClient apiClient;

  setUp(() {
    adapter = _ThumbAdapter();
    apiClient = ApiClient(
      tokenStore: _FakeTokenStore(),
      sessionExpiryNotifier: SessionExpiryNotifier(),
      retryPolicy: RetryPolicy(delayFn: (_) async {}),
      dio: Dio()..httpClientAdapter = adapter,
    )..configureBaseUrl('http://test.local');
  });

  test('mimeTypeHasThumbnail solo acepta imagen, vídeo y PDF', () {
    expect(mimeTypeHasThumbnail('image/jpeg'), isTrue);
    expect(mimeTypeHasThumbnail('video/mp4'), isTrue);
    expect(mimeTypeHasThumbnail('application/pdf'), isTrue);
    expect(mimeTypeHasThumbnail('text/plain'), isFalse);
    expect(mimeTypeHasThumbnail(null), isFalse);
  });

  test('descarga una vez y sirve de la caché después', () async {
    final service = ThumbnailService(apiClient: apiClient);

    final first = await service.load('a', 'h1');
    final second = await service.load('a', 'h1');

    expect(first, [1, 2, 3]);
    expect(second, [1, 2, 3]);
    expect(service.cached('a', 'h1'), [1, 2, 3]);
    expect(adapter.requests, ['/files/a/thumbnail']);
  });

  test('si el contenido cambia (otro SHA-256) vuelve a pedirla', () async {
    final service = ThumbnailService(apiClient: apiClient);

    await service.load('a', 'h1');
    await service.load('a', 'h2');

    expect(adapter.requests, hasLength(2));
  });

  test('un 404 se recuerda y no se vuelve a pedir', () async {
    adapter.statusFor = (_) => 404;
    final service = ThumbnailService(apiClient: apiClient);

    expect(await service.load('a', 'h1'), isNull);
    expect(service.isKnownMissing('a', 'h1'), isTrue);
    expect(await service.load('a', 'h1'), isNull);

    expect(adapter.requests, hasLength(1));
  });

  test('un 503 (contención transitoria) no se recuerda', () async {
    adapter.statusFor = (_) => 503;
    final service = ThumbnailService(apiClient: apiClient);

    expect(await service.load('a', 'h1'), isNull);
    expect(service.isKnownMissing('a', 'h1'), isFalse);
    adapter.statusFor = (_) => 200;
    expect(await service.load('a', 'h1'), [1, 2, 3]);

    expect(adapter.requests, hasLength(2));
  });

  test(
    'limita las peticiones simultáneas y descarta lo que ya no se ve',
    () async {
      adapter.gate = Completer<void>();
      final service = ThumbnailService(apiClient: apiClient, maxConcurrent: 2);
      final wanted = {'a': true, 'b': true, 'c': false, 'd': true};

      final futures = [
        for (final id in wanted.keys)
          service.load(id, 'h', stillWanted: () => wanted[id]!),
      ];
      // El interceptor del ApiClient tarda unos ticks en dejar pasar cada
      // petición: se espera a que lleguen las que caben y un poco más, para
      // ver que ninguna otra se cuela.
      for (var i = 0; i < 100 && adapter.inFlight < 2; i++) {
        await Future<void>.delayed(const Duration(milliseconds: 1));
      }
      await Future<void>.delayed(const Duration(milliseconds: 20));
      expect(adapter.inFlight, 2);

      adapter.gate!.complete();
      final results = await Future.wait(futures);

      expect(adapter.maxInFlight, 2);
      expect(results[2], isNull);
      expect(adapter.requests, isNot(contains('/files/c/thumbnail')));
      expect(adapter.requests, hasLength(3));
    },
  );

  test('expulsa las menos recientes al pasar del límite de bytes', () async {
    adapter.bodyFor = (_) => Uint8List(40);
    final service = ThumbnailService(apiClient: apiClient, maxCacheBytes: 100);

    await service.load('a', 'h');
    await service.load('b', 'h');
    service.cached('a', 'h'); // «a» pasa a ser la más reciente
    await service.load('c', 'h');

    expect(service.cached('a', 'h'), isNotNull);
    expect(service.cached('b', 'h'), isNull);
    expect(service.cached('c', 'h'), isNotNull);
  });

  test(
    'clear vacía la caché y lo que estaba en vuelo no la repuebla',
    () async {
      adapter.gate = Completer<void>();
      final service = ThumbnailService(apiClient: apiClient);

      final pending = service.load('a', 'h');
      await Future<void>.delayed(Duration.zero);
      service.clear();
      adapter.gate!.complete();
      await pending;

      expect(service.cached('a', 'h'), isNull);
    },
  );
}
