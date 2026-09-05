import 'package:dio/dio.dart';

import '../storage/token_store.dart';
import 'api_exception.dart';
import 'retry_policy.dart';
import 'session_expiry_notifier.dart';

/// Cliente HTTP central hacia la API REST de NexusCloud (`/api/v1`), con un
/// único interceptor responsable de (ver ADR-009):
///
///  1. Adjuntar `Authorization: Bearer <token>` leyendo [TokenStore].
///  2. Traducir cualquier respuesta de error `{"error":{"code","message"}}`
///     en una [ApiException] — repositorios y presentación nunca ven un
///     `DioException` crudo.
///  3. Detectar sesión caducada: un 401 a una petición que SÍ llevaba
///     `Authorization` dispara [SessionExpiryNotifier] (un 401 al propio
///     login no cuenta — ahí simplemente la contraseña es incorrecta).
///  4. Reintentar 429 con backoff antes de propagar `rate_limited` (el
///     servidor no manda `Retry-After`).
///
/// La URL base NUNCA es una constante (§48: no asumir puertos fijos) — se
/// fija en tiempo de ejecución con [configureBaseUrl].
class ApiClient {
  ApiClient({
    required TokenStore tokenStore,
    required SessionExpiryNotifier sessionExpiryNotifier,
    RetryPolicy? retryPolicy,
    Dio? dio,
  })  : _tokenStore = tokenStore,
        _sessionExpiryNotifier = sessionExpiryNotifier,
        _retryPolicy = retryPolicy ?? RetryPolicy(),
        // Sin timeout, una URL de servidor mal escrita o inalcanzable deja
        // el spinner de login girando para siempre sin ningún error visible
        // (encontrado probando la app real contra un servidor real, no en
        // los tests unitarios, que siempre usan un adaptador falso que
        // responde al instante). connectTimeout cubre no poder ni siquiera
        // establecer la conexión; receiveTimeout cubre un servidor que
        // acepta la conexión pero nunca responde.
        _dio = dio ??
            Dio(
              BaseOptions(
                connectTimeout: const Duration(seconds: 15),
                receiveTimeout: const Duration(seconds: 30),
              ),
            ) {
    _dio.interceptors.add(
      InterceptorsWrapper(onRequest: _onRequest, onError: _onError),
    );
  }

  final TokenStore _tokenStore;
  final SessionExpiryNotifier _sessionExpiryNotifier;
  final RetryPolicy _retryPolicy;
  final Dio _dio;

  /// [baseUrl] es la dirección del servidor tal como la escribe el usuario
  /// (p.ej. `https://nexuscloud.midominio.com`, sin ruta) — todas las
  /// rutas de la API viven bajo `/api/v1` (`internal/api/v1/router.go`,
  /// `internal/server/server.go`'s `root.Mount("/api/v1", ...)`), así que
  /// ese prefijo se añade aquí una sola vez en vez de repetirlo en cada
  /// data source.
  void configureBaseUrl(String baseUrl) {
    final trimmed = baseUrl.endsWith('/')
        ? baseUrl.substring(0, baseUrl.length - 1)
        : baseUrl;
    _dio.options.baseUrl = '$trimmed/api/v1';
  }

  /// Punto de entrada único para los data sources — garantiza que
  /// cualquier fallo llega a quien llama como [ApiException], nunca como
  /// `DioException` crudo.
  Future<Response<T>> request<T>(
    Future<Response<T>> Function(Dio dio) call,
  ) async {
    try {
      return await call(_dio);
    } on DioException catch (e) {
      final error = e.error;
      if (error is ApiException) throw error;
      throw ApiException.unknown();
    }
  }

  void _onRequest(
    RequestOptions options,
    RequestInterceptorHandler handler,
  ) async {
    final token = await _tokenStore.read();
    if (token != null) {
      options.headers['Authorization'] = 'Bearer $token';
    }
    handler.next(options);
  }

  /// El contador de reintentos viaja en `requestOptions.extra`, no en una
  /// variable local: `_dio.fetch` para reintentar vuelve a pasar por ESTE
  /// mismo interceptor (es el mismo `_dio`), así que una variable local
  /// se reiniciaría a 0 en cada nivel de recursión y compondría
  /// reintentos (hasta 3×3). Guardar el intento en `extra` -- que es el
  /// mismo objeto `RequestOptions` en cada nivel -- acota la recursión
  /// total a `maxAttempts`, y cada nivel que reintenta simplemente
  /// reenvía (`handler.next`) el error ya mapeado por el nivel más
  /// profundo, sin volver a mapearlo.
  Future<void> _onError(
    DioException err,
    ErrorInterceptorHandler handler,
  ) async {
    final response = err.response;

    if (response == null) {
      handler.next(err.copyWith(error: ApiException.network()));
      return;
    }

    if (response.statusCode == 429) {
      final attempt = (err.requestOptions.extra['retry_attempt'] as int?) ?? 0;
      if (_retryPolicy.shouldRetry(attempt)) {
        await _retryPolicy.waitBeforeRetry(attempt);
        err.requestOptions.extra['retry_attempt'] = attempt + 1;
        try {
          final retryResponse = await _dio.fetch(err.requestOptions);
          handler.resolve(retryResponse);
        } on DioException catch (retryErr) {
          handler.next(retryErr);
        }
        return;
      }
    }

    final wasAuthenticated =
        err.requestOptions.headers.containsKey('Authorization');
    if (response.statusCode == 401 && wasAuthenticated) {
      _sessionExpiryNotifier.notify();
    }

    handler.next(err.copyWith(error: _toApiException(response)));
  }

  ApiException _toApiException(Response<dynamic> response) {
    final data = response.data;
    if (data is Map && data['error'] is Map) {
      final error = data['error'] as Map;
      return ApiException(
        code: (error['code'] as String?) ?? 'unknown_error',
        message: (error['message'] as String?) ??
            'No se pudo completar la operación.',
        statusCode: response.statusCode,
      );
    }
    return ApiException.unknown();
  }

  void dispose() => _dio.close();
}
