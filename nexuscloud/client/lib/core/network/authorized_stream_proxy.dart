import 'dart:convert';
import 'dart:io';
import 'dart:math';

/// Proxy HTTP en 127.0.0.1 para que el reproductor (libmpv) reproduzca un
/// archivo del servidor SIN conocer el token de sesión.
///
/// Por qué existe: media_kit aplica las cabeceras como la propiedad GLOBAL
/// `http-header-fields` de mpv, y FFmpeg reenvía esas cabeceras a todo lo
/// que abre a partir de ese medio. Un «vídeo» que en realidad es una lista
/// HLS con segmentos en otro host recibiría el `Authorization: Bearer` y
/// el atacante tendría la sesión (hallazgo ALTO de la revisión de
/// seguridad). Con este proxy, mpv solo ve una URL local; el token lo añade
/// el proxy y solo hacia [upstream], el recurso exacto del servidor.
///
/// Superficie mínima: escucha solo en loopback, en un puerto aleatorio,
/// responde únicamente a GET/HEAD en una ruta secreta aleatoria (otro
/// proceso del equipo no puede adivinarla), no sigue redirecciones y solo
/// reenvía `Range`. Vive lo que dura la vista previa ([close]).
class AuthorizedStreamProxy {
  AuthorizedStreamProxy._(
    this._server,
    this._client,
    this._upstream,
    this._headers,
    this._secretPath,
  );

  static Future<AuthorizedStreamProxy> start({
    required Uri upstream,
    required Map<String, String> headers,
    HttpClient? client,
  }) async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final random = Random.secure();
    final secret = base64UrlEncode(
      List<int>.generate(32, (_) => random.nextInt(256)),
    ).replaceAll('=', '');
    final proxy = AuthorizedStreamProxy._(
      server,
      (client ?? HttpClient())
        // Los bytes tal cual: el reproductor necesita Content-Length y
        // Content-Range coherentes con lo que recibe.
        ..autoUncompress = false,
      upstream,
      Map.unmodifiable(headers),
      '/$secret',
    );
    server.listen(proxy._handle);
    return proxy;
  }

  final HttpServer _server;
  final HttpClient _client;
  final Uri _upstream;
  final Map<String, String> _headers;
  final String _secretPath;

  /// URL local que se le da al reproductor. No contiene el token.
  Uri get uri => Uri(
    scheme: 'http',
    host: InternetAddress.loopbackIPv4.address,
    port: _server.port,
    path: _secretPath,
  );

  static const _forwardedResponseHeaders = [
    HttpHeaders.contentTypeHeader,
    HttpHeaders.contentRangeHeader,
    HttpHeaders.acceptRangesHeader,
  ];

  Future<void> _handle(HttpRequest request) async {
    final response = request.response;
    try {
      final method = request.method;
      if (!_sameSecret(request.uri.path) ||
          (method != 'GET' && method != 'HEAD')) {
        response.statusCode = HttpStatus.notFound;
        await response.close();
        return;
      }

      final upstreamRequest = await _client.openUrl(method, _upstream);
      upstreamRequest.followRedirects = false;
      _headers.forEach(upstreamRequest.headers.set);
      final range = request.headers.value(HttpHeaders.rangeHeader);
      if (range != null) {
        upstreamRequest.headers.set(HttpHeaders.rangeHeader, range);
      }
      final upstreamResponse = await upstreamRequest.close();

      response.statusCode = upstreamResponse.statusCode;
      for (final name in _forwardedResponseHeaders) {
        final value = upstreamResponse.headers.value(name);
        if (value != null) response.headers.set(name, value);
      }
      if (upstreamResponse.contentLength >= 0) {
        response.contentLength = upstreamResponse.contentLength;
      }
      if (method == 'HEAD') {
        await upstreamResponse.drain<void>();
      } else {
        await response.addStream(upstreamResponse);
      }
      await response.close();
    } catch (_) {
      // El reproductor verá un corte y mostrará su error genérico; aquí no
      // hay nada útil que contar (ni se debe: podría incluir la URL).
      try {
        response.statusCode = HttpStatus.badGateway;
      } on StateError {
        // Las cabeceras ya se enviaron: solo queda cerrar.
      }
      await response.close().catchError((_) {});
    }
  }

  /// Comparación en tiempo constante: el tiempo de respuesta no revela
  /// cuántos caracteres de la ruta secreta se acertaron.
  bool _sameSecret(String path) {
    if (path.length != _secretPath.length) return false;
    var diff = 0;
    for (var i = 0; i < path.length; i++) {
      diff |= path.codeUnitAt(i) ^ _secretPath.codeUnitAt(i);
    }
    return diff == 0;
  }

  Future<void> close() async {
    await _server.close(force: true);
    _client.close(force: true);
  }
}
