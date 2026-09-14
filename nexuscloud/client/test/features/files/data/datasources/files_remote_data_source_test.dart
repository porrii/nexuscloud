import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';
import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/network/api_client.dart';
import 'package:nexuscloud_client/core/network/api_exception.dart';
import 'package:nexuscloud_client/core/network/retry_policy.dart';
import 'package:nexuscloud_client/core/network/session_expiry_notifier.dart';
import 'package:nexuscloud_client/core/storage/token_store.dart';
import 'package:nexuscloud_client/features/files/data/datasources/files_remote_data_source.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_entry.dart';

class _FakeTokenStore implements TokenStore {
  @override
  Future<void> save(String token) async {}

  @override
  Future<String?> read() async => 'a-token';

  @override
  Future<void> clear() async {}
}

/// Adaptador HTTP falso que reimplementa en miniatura la decisión que
/// toma el backend real (`internal/api/v1/range.go`, ADR-010 §41): sirve
/// [fullContent] completo (200) o, si la petición trae
/// `Range: bytes=N-`, la porción desde N con 206 -- sin abrir ningún
/// socket real, mismo criterio que `_FakeHttpClientAdapter` de
/// `api_client_test.dart`.
class _RangeAwareAdapter implements HttpClientAdapter {
  _RangeAwareAdapter(this.fullContent);

  final List<int> fullContent;
  final List<RequestOptions> requests = [];

  /// Si se pone, la PRÓXIMA respuesta corta el stream tras exactamente
  /// este número de bytes (a partir del offset que corresponda) -- para
  /// simular un corte de red real a mitad de descarga. Se consume solo
  /// una vez.
  int? failAfterBytesOnce;

  /// Si es true, la PRÓXIMA petición responde 416 con el mismo sobre JSON
  /// que escribe el backend real -- para probar la invalidación del
  /// `.part`. Se consume solo una vez.
  bool respondUnsatisfiableOnce = false;

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    requests.add(options);

    if (respondUnsatisfiableOnce) {
      respondUnsatisfiableOnce = false;
      final body = jsonEncode({
        'error': {
          'code': 'range_not_satisfiable',
          'message': 'El rango solicitado no es válido.',
        },
      });
      return ResponseBody.fromString(
        body,
        416,
        headers: {
          Headers.contentTypeHeader: [Headers.jsonContentType],
        },
      );
    }

    final rangeHeader = options.headers['range'] as String?;
    var start = 0;
    var status = 200;
    if (rangeHeader != null && rangeHeader.startsWith('bytes=')) {
      start = int.parse(rangeHeader.substring('bytes='.length).replaceAll('-', ''));
      status = 206;
    }
    final slice = fullContent.sublist(start);

    final failAfter = failAfterBytesOnce;
    if (failAfter != null) {
      failAfterBytesOnce = null;
      return ResponseBody(_flakyStream(slice, failAfter), status);
    }
    return ResponseBody.fromBytes(slice, status);
  }

  Stream<Uint8List> _flakyStream(List<int> slice, int bytesBeforeFailure) async* {
    yield Uint8List.fromList(slice.sublist(0, bytesBeforeFailure));
    throw Exception('conexión perdida a mitad de la transmisión (simulada)');
  }

  @override
  void close({bool force = false}) {}
}

FileEntry _fileEntry({required String id, required String sha256Hex, required int sizeBytes}) {
  final ts = DateTime.utc(2026, 1, 1);
  return FileEntry(
    id: id,
    parentPath: '/',
    name: 'archivo.bin',
    sizeBytes: sizeBytes,
    sha256: sha256Hex,
    mimeType: 'application/octet-stream',
    createdAt: ts,
    updatedAt: ts,
  );
}

void main() {
  late Directory tempDir;
  late _RangeAwareAdapter adapter;
  late ApiClient apiClient;
  late FilesRemoteDataSource dataSource;
  late List<int> content;
  late String contentHash;
  late String savePath;

  setUp(() {
    tempDir = Directory.systemTemp.createTempSync('nexuscloud_download_resume_');
    // Contenido con relleno reconocible para poder inspeccionar a mano un
    // fallo de test ("¿qué bytes llegaron de verdad?"), no aleatorio.
    content = List<int>.generate(200, (i) => i % 256);
    contentHash = sha256.convert(content).toString();
    savePath = '${tempDir.path}/descargado.bin';

    adapter = _RangeAwareAdapter(content);
    final dio = Dio()..httpClientAdapter = adapter;
    apiClient = ApiClient(
      tokenStore: _FakeTokenStore(),
      sessionExpiryNotifier: SessionExpiryNotifier(),
      retryPolicy: RetryPolicy(delayFn: (_) async {}),
      dio: dio,
    );
    apiClient.configureBaseUrl('http://test.local');
    dataSource = FilesRemoteDataSource(apiClient: apiClient);
  });

  tearDown(() {
    if (tempDir.existsSync()) tempDir.deleteSync(recursive: true);
  });

  /// Cualquier `.part` que pudiera quedar en el directorio de [savePath].
  List<FileSystemEntity> partFilesLeft() => tempDir
      .listSync()
      .where((e) => e is File && e.path.endsWith('.part'))
      .toList();

  test('descarga normal (sin interrupción) produce el archivo final y ningún .part', () async {
    final file = _fileEntry(id: 'f1', sha256Hex: contentHash, sizeBytes: content.length);

    await dataSource.downloadFile(file: file, saveToPath: savePath);

    expect(await File(savePath).readAsBytes(), content);
    expect(partFilesLeft(), isEmpty);
    expect(adapter.requests, hasLength(1));
    expect(adapter.requests.single.headers.containsKey('range'), isFalse);
  });

  test('un corte a mitad dispara una excepción y deja el .part con lo descargado hasta ese punto', () async {
    final file = _fileEntry(id: 'f1', sha256Hex: contentHash, sizeBytes: content.length);
    adapter.failAfterBytesOnce = 50;

    await expectLater(
      dataSource.downloadFile(file: file, saveToPath: savePath),
      throwsA(anything),
    );

    expect(File(savePath).existsSync(), isFalse, reason: 'el destino final nunca se toca hasta completar');
    final parts = partFilesLeft();
    expect(parts, hasLength(1));
    expect(await File(parts.single.path).readAsBytes(), content.sublist(0, 50));
  });

  test(
    'un segundo intento tras el corte reanuda con Range desde el offset guardado y ensambla el archivo completo',
    () async {
      final file = _fileEntry(id: 'f1', sha256Hex: contentHash, sizeBytes: content.length);
      adapter.failAfterBytesOnce = 50;
      await expectLater(
        dataSource.downloadFile(file: file, saveToPath: savePath),
        throwsA(anything),
      );
      expect(partFilesLeft(), hasLength(1));

      await dataSource.downloadFile(file: file, saveToPath: savePath);

      expect(await File(savePath).readAsBytes(), content);
      expect(partFilesLeft(), isEmpty, reason: 'el .part se renombra al destino final tras verificar integridad');
      expect(adapter.requests, hasLength(2));
      expect(adapter.requests[1].headers['range'], 'bytes=50-');
    },
  );

  test(
    'un hash esperado distinto al del .part existente lo descarta y descarga de cero',
    () async {
      // Deja a mano un .part "de otra versión" del mismo destino, con un
      // hash que NO coincide con el que se va a pedir ahora.
      final staleHash = sha256.convert(utf8.encode('otra version')).toString();
      final staleHash8 = staleHash.substring(0, 8);
      final staleFile = File('$savePath.$staleHash8.part');
      await staleFile.writeAsBytes(List<int>.filled(30, 9));

      final file = _fileEntry(id: 'f1', sha256Hex: contentHash, sizeBytes: content.length);
      await dataSource.downloadFile(file: file, saveToPath: savePath);

      expect(await File(savePath).readAsBytes(), content);
      expect(staleFile.existsSync(), isFalse, reason: 'el .part obsoleto se descarta, no se reutiliza');
      // Se descargó de cero -- sin cabecera Range, pese al offset que
      // tenía el .part descartado.
      expect(adapter.requests.single.headers.containsKey('range'), isFalse);
    },
  );

  test(
    'un 416 al reanudar descarta el .part y reintenta desde cero automáticamente',
    () async {
      final file = _fileEntry(id: 'f1', sha256Hex: contentHash, sizeBytes: content.length);
      adapter.failAfterBytesOnce = 50;
      await expectLater(
        dataSource.downloadFile(file: file, saveToPath: savePath),
        throwsA(anything),
      );
      expect(partFilesLeft(), hasLength(1));

      // El segundo intento normalmente reanudaría con Range -- se fuerza
      // que ESE intento reciba 416 (el remoto "encogió").
      adapter.respondUnsatisfiableOnce = true;

      await dataSource.downloadFile(file: file, saveToPath: savePath);

      expect(await File(savePath).readAsBytes(), content);
      expect(partFilesLeft(), isEmpty);
      // 1ª (falla a mitad) + 2ª (416) + 3ª (desde cero, sin Range).
      expect(adapter.requests, hasLength(3));
      expect(adapter.requests[1].headers['range'], 'bytes=50-');
      expect(adapter.requests[2].headers.containsKey('range'), isFalse);
    },
  );

  test('un hash final que no coincide (incluso tras reanudar) lanza integrity_mismatch y borra el .part', () async {
    final wrongHash = sha256.convert(utf8.encode('contenido distinto')).toString();
    final file = _fileEntry(id: 'f1', sha256Hex: wrongHash, sizeBytes: content.length);

    await expectLater(
      dataSource.downloadFile(file: file, saveToPath: savePath),
      throwsA(isA<ApiException>().having((e) => e.code, 'code', 'integrity_mismatch')),
    );

    expect(File(savePath).existsSync(), isFalse);
    expect(partFilesLeft(), isEmpty);
  });
}
