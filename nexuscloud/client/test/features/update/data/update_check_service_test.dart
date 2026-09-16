import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/network/api_client.dart';
import 'package:nexuscloud_client/core/network/retry_policy.dart';
import 'package:nexuscloud_client/core/network/session_expiry_notifier.dart';
import 'package:nexuscloud_client/core/storage/token_store.dart';
import 'package:nexuscloud_client/features/update/data/update_check_service.dart';
import 'package:nexuscloud_client/features/update/domain/entities/update_check_result.dart';

class _FakeTokenStore implements TokenStore {
  @override
  Future<void> save(String token) async {}
  @override
  Future<String?> read() async => null;
  @override
  Future<void> clear() async {}
}

/// Responde el cuerpo/estado que se le configure para
/// `/public/client-updates/releases.json` -- sin abrir ningún socket real,
/// mismo criterio que `_RangeAwareAdapter` de
/// `files_remote_data_source_test.dart`.
class _FeedAdapter implements HttpClientAdapter {
  int status = 200;
  String body = '{"Assets":[]}';

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    if (status == 404) {
      final err = jsonEncode({
        'error': {'code': 'not_found', 'message': 'No disponible.'},
      });
      return ResponseBody.fromString(
        err,
        404,
        headers: {Headers.contentTypeHeader: [Headers.jsonContentType]},
      );
    }
    return ResponseBody.fromString(
      body,
      status,
      headers: {Headers.contentTypeHeader: [Headers.jsonContentType]},
    );
  }

  @override
  void close({bool force = false}) {}
}

String _feedWithFull(String version, {String fileName = 'NexusCloud-x-full.nupkg'}) =>
    jsonEncode({
      'Assets': [
        {
          'PackageId': 'NexusCloud',
          'Version': version,
          'Type': 'Full',
          'FileName': fileName,
          'SHA256': 'DEADBEEF',
          'Size': 12345,
        },
      ],
    });

void main() {
  late _FeedAdapter adapter;
  late ApiClient apiClient;

  ApiClient buildClient() {
    final dio = Dio()..httpClientAdapter = adapter;
    final client = ApiClient(
      tokenStore: _FakeTokenStore(),
      sessionExpiryNotifier: SessionExpiryNotifier(),
      retryPolicy: RetryPolicy(delayFn: (_) async {}),
      dio: dio,
    );
    client.configureBaseUrl('http://test.local');
    return client;
  }

  setUp(() {
    adapter = _FeedAdapter();
    apiClient = buildClient();
  });

  test('versión del feed más reciente -> updateAvailable con el asset Full', () async {
    adapter.body = _feedWithFull('9.9.9');
    final service = UpdateCheckService(apiClient: apiClient, currentVersion: '1.0.0');

    final result = await service.check();

    expect(result.status, UpdateCheckStatus.updateAvailable);
    expect(result.available!.version, '9.9.9');
    expect(result.available!.fileName, 'NexusCloud-x-full.nupkg');
  });

  test('misma versión -> upToDate', () async {
    adapter.body = _feedWithFull('1.0.0');
    final service = UpdateCheckService(apiClient: apiClient, currentVersion: '1.0.0');

    expect((await service.check()).status, UpdateCheckStatus.upToDate);
  });

  test('versión del feed menor -> upToDate (nunca "actualiza" hacia atrás)', () async {
    adapter.body = _feedWithFull('0.9.0');
    final service = UpdateCheckService(apiClient: apiClient, currentVersion: '1.0.0');

    expect((await service.check()).status, UpdateCheckStatus.upToDate);
  });

  test('comparación numérica, no textual: 1.10.0 es más nueva que 1.9.0', () async {
    adapter.body = _feedWithFull('1.10.0');
    final service = UpdateCheckService(apiClient: apiClient, currentVersion: '1.9.0');

    expect((await service.check()).status, UpdateCheckStatus.updateAvailable);
  });

  test('servidor sin la función activada (404) -> unavailable, sin lanzar', () async {
    adapter.status = 404;
    final service = UpdateCheckService(apiClient: apiClient, currentVersion: '1.0.0');

    expect((await service.check()).status, UpdateCheckStatus.unavailable);
  });

  test('feed solo con assets Delta (sin Full) -> unavailable', () async {
    adapter.body = jsonEncode({
      'Assets': [
        {
          'PackageId': 'NexusCloud',
          'Version': '9.9.9',
          'Type': 'Delta',
          'FileName': 'delta.nupkg',
          'SHA256': 'X',
          'Size': 1,
        },
      ],
    });
    final service = UpdateCheckService(apiClient: apiClient, currentVersion: '1.0.0');

    expect((await service.check()).status, UpdateCheckStatus.unavailable);
  });
}
