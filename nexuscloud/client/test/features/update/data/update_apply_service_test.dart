import 'dart:io';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';
import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/network/api_client.dart';
import 'package:nexuscloud_client/core/network/retry_policy.dart';
import 'package:nexuscloud_client/core/network/session_expiry_notifier.dart';
import 'package:nexuscloud_client/core/storage/token_store.dart';
import 'package:nexuscloud_client/features/update/data/update_apply_service.dart';
import 'package:nexuscloud_client/features/update/domain/entities/update_asset.dart';
import 'package:path_provider_platform_interface/path_provider_platform_interface.dart';
import 'package:plugin_platform_interface/plugin_platform_interface.dart';

class _FakeTokenStore implements TokenStore {
  @override
  Future<void> save(String token) async {}
  @override
  Future<String?> read() async => null;
  @override
  Future<void> clear() async {}
}

/// Sirve [content] tal cual para cualquier petición -- basta para probar
/// stageUpdate, que solo pide una URL fija por asset.
class _AssetAdapter implements HttpClientAdapter {
  _AssetAdapter(this.content);
  final List<int> content;

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    return ResponseBody.fromBytes(content, 200);
  }

  @override
  void close({bool force = false}) {}
}

class _FakePathProviderPlatform extends PathProviderPlatform
    with MockPlatformInterfaceMixin {
  _FakePathProviderPlatform(this.tempPath);
  final String tempPath;

  @override
  Future<String?> getTemporaryPath() async => tempPath;
}

void main() {
  late Directory tempDir;
  late _AssetAdapter adapter;
  late ApiClient apiClient;
  late List<int> content;
  late String contentHashHex;

  setUp(() {
    tempDir = Directory.systemTemp.createTempSync('nexuscloud_update_apply_');
    PathProviderPlatform.instance = _FakePathProviderPlatform(tempDir.path);

    content = List<int>.generate(500, (i) => i % 256);
    contentHashHex = sha256.convert(content).toString();

    adapter = _AssetAdapter(content);
    final dio = Dio()..httpClientAdapter = adapter;
    apiClient = ApiClient(
      tokenStore: _FakeTokenStore(),
      sessionExpiryNotifier: SessionExpiryNotifier(),
      retryPolicy: RetryPolicy(delayFn: (_) async {}),
      dio: dio,
    );
    apiClient.configureBaseUrl('http://test.local');
  });

  tearDown(() {
    if (tempDir.existsSync()) tempDir.deleteSync(recursive: true);
  });

  UpdateAsset asset({required String sha256Hex}) => UpdateAsset(
        packageId: 'NexusCloud',
        version: '9.9.9',
        type: 'Full',
        fileName: 'NexusCloud-9.9.9-full.nupkg',
        sha256: sha256Hex,
        size: content.length,
      );

  test('stageUpdate descarga y, con el hash correcto, devuelve la ruta con el contenido', () async {
    final service = UpdateApplyService(apiClient: apiClient);

    final path = await service.stageUpdate(asset(sha256Hex: contentHashHex));

    expect(await File(path).readAsBytes(), content);
  });

  test('stageUpdate con hash incorrecto lanza y borra el fichero a medias', () async {
    final service = UpdateApplyService(apiClient: apiClient);

    await expectLater(
      () => service.stageUpdate(asset(sha256Hex: 'no-coincide-con-nada')),
      throwsA(isA<UpdateApplyException>()),
    );
    expect(
      File('${tempDir.path}/NexusCloud-9.9.9-full.nupkg').existsSync(),
      isFalse,
    );
  });

  test('la comparación del hash no distingue mayúsculas/minúsculas', () async {
    final service = UpdateApplyService(apiClient: apiClient);

    final path = await service.stageUpdate(
      asset(sha256Hex: contentHashHex.toLowerCase()),
    );

    expect(await File(path).exists(), isTrue);
  });

  test(
    'canApplyUpdates es false cuando no hay Update.exe junto al ejecutable actual '
    '(el caso real de "flutter test"/"flutter run" -- Update.exe solo existe en una '
    'instalación real de Velopack, verificado empaquetando una app de prueba de verdad)',
    () {
      final service = UpdateApplyService(apiClient: apiClient);
      expect(service.canApplyUpdates, isFalse);
    },
  );
}
