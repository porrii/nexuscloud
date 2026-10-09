import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/network/authorized_stream_proxy.dart';
import 'package:nexuscloud_client/features/files/presentation/preview/media_preview.dart';

/// Servidor «de NexusCloud» falso en loopback: registra lo que recibe y
/// responde con un cuerpo fijo, respetando Range de forma simple.
class _Upstream {
  late HttpServer server;
  final requests = <({String method, String path, String? auth, String? range})>[];

  Future<void> start() async {
    server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    server.listen((req) async {
      requests.add((
        method: req.method,
        path: req.uri.path,
        auth: req.headers.value(HttpHeaders.authorizationHeader),
        range: req.headers.value(HttpHeaders.rangeHeader),
      ));
      if (req.uri.path == '/redirect') {
        req.response
          ..statusCode = HttpStatus.found
          ..headers.set(HttpHeaders.locationHeader, 'http://ejemplo.invalid/');
        await req.response.close();
        return;
      }
      const body = '0123456789';
      final range = req.headers.value(HttpHeaders.rangeHeader);
      if (range == 'bytes=2-4') {
        req.response
          ..statusCode = HttpStatus.partialContent
          ..headers.set(HttpHeaders.contentRangeHeader, 'bytes 2-4/10')
          ..headers.contentType = ContentType('video', 'mp4')
          ..contentLength = 3
          ..write(body.substring(2, 5));
      } else {
        req.response
          ..headers.contentType = ContentType('video', 'mp4')
          ..contentLength = body.length
          ..write(body);
      }
      await req.response.close();
    });
  }

  Uri uri(String path) => Uri.parse('http://127.0.0.1:${server.port}$path');
}

Future<({int status, String body, HttpHeaders headers})> _get(
  Uri uri, {
  String method = 'GET',
  String? range,
}) async {
  final client = HttpClient();
  try {
    final req = await client.openUrl(method, uri);
    req.followRedirects = false;
    if (range != null) req.headers.set(HttpHeaders.rangeHeader, range);
    final res = await req.close();
    final body = await utf8.decodeStream(res);
    return (status: res.statusCode, body: body, headers: res.headers);
  } finally {
    client.close(force: true);
  }
}

void main() {
  late _Upstream upstream;
  late AuthorizedStreamProxy proxy;

  setUp(() async {
    upstream = _Upstream();
    await upstream.start();
    proxy = await AuthorizedStreamProxy.start(
      upstream: upstream.uri('/api/v1/files/abc'),
      headers: {'Authorization': 'Bearer secreto'},
    );
  });

  tearDown(() async {
    await proxy.close();
    await upstream.server.close(force: true);
  });

  test('la URL local no contiene el token y escucha solo en loopback', () {
    expect(proxy.uri.toString(), isNot(contains('secreto')));
    expect(proxy.uri.host, '127.0.0.1');
    // Ruta aleatoria larga: otro proceso del equipo no puede adivinarla.
    expect(proxy.uri.path.length, greaterThan(40));
  });

  test('añade el token solo hacia el recurso del servidor y reenvía Range', () async {
    final full = await _get(proxy.uri);
    expect(full.status, 200);
    expect(full.body, '0123456789');

    final partial = await _get(proxy.uri, range: 'bytes=2-4');
    expect(partial.status, 206);
    expect(partial.body, '234');
    expect(partial.headers.value(HttpHeaders.contentRangeHeader), 'bytes 2-4/10');

    expect(upstream.requests.map((r) => r.path), everyElement('/api/v1/files/abc'));
    expect(upstream.requests.map((r) => r.auth), everyElement('Bearer secreto'));
    expect(upstream.requests.last.range, 'bytes=2-4');
  });

  test('otra ruta u otro método no llegan al servidor', () async {
    final wrongPath = await _get(proxy.uri.replace(path: '/otra'));
    final wrongMethod = await _get(proxy.uri, method: 'POST');

    expect(wrongPath.status, 404);
    expect(wrongMethod.status, 404);
    expect(upstream.requests, isEmpty);
  });

  test('no sigue redirecciones (el token no viaja a otro host)', () async {
    await proxy.close();
    proxy = await AuthorizedStreamProxy.start(
      upstream: upstream.uri('/redirect'),
      headers: {'Authorization': 'Bearer secreto'},
    );

    final res = await _get(proxy.uri);

    expect(res.status, HttpStatus.found);
    // Tampoco reenvía a dónde apuntaba la redirección.
    expect(res.headers.value(HttpHeaders.locationHeader), isNull);
    expect(upstream.requests, hasLength(1));
  });

  group('looksLikeTextNotMedia', () {
    test('rechaza listas de reproducción disfrazadas', () {
      expect(looksLikeTextNotMedia(utf8.encode('#EXTM3U\n#EXTINF:1,\nhttp://x/a.ts\n')), isTrue);
      expect(looksLikeTextNotMedia(utf8.encode('<?xml version="1.0"?><MPD>')), isTrue);
      expect(looksLikeTextNotMedia(utf8.encode('[playlist]\nFile1=http://x\n')), isTrue);
    });

    test('acepta contenedores binarios reales', () {
      // Cabecera MP4 (ftyp) y MKV (EBML).
      expect(looksLikeTextNotMedia([0, 0, 0, 0x18, 0x66, 0x74, 0x79, 0x70]), isFalse);
      expect(looksLikeTextNotMedia([0x1A, 0x45, 0xDF, 0xA3, 0x9F]), isFalse);
      expect(looksLikeTextNotMedia(const []), isFalse);
    });
  });
}
