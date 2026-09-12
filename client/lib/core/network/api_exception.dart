/// Excepción tipada para cualquier error de la API de NexusCloud.
///
/// Repositorios y presentación solo ven esto — nunca un `DioException`
/// crudo (§167: reduce a un único punto el riesgo de que un detalle interno
/// llegue a la UI en vez de al log de depuración local).
class ApiException implements Exception {
  const ApiException({
    required this.code,
    required this.message,
    this.statusCode,
  });

  /// Código estable de error del servidor (p.ej. "unauthorized",
  /// "totp_required", "rate_limited"). La presentación debe ramificar sobre
  /// esto, nunca sobre [message] (texto humano en español, puede cambiar).
  final String code;

  /// Mensaje del servidor, seguro para mostrar tal cual (§170: el backend
  /// nunca filtra detalles internos en sus mensajes de error).
  final String message;

  /// Código HTTP, si la excepción vino de una respuesta real del servidor
  /// (null para errores de red/timeout, donde no hubo respuesta).
  final int? statusCode;

  /// No hubo respuesta del servidor (timeout, DNS, conexión rechazada...).
  /// A diferencia de un 401/403 confirmado, esto NO implica que la sesión
  /// sea inválida — ver el manejo de auto-login en AuthRepositoryImpl.
  factory ApiException.network([
    String message = 'No se pudo conectar con el servidor.',
  ]) =>
      ApiException(code: 'network_error', message: message);

  /// Respuesta que no encaja en el sobre de error esperado
  /// (`{"error":{"code","message"}}`) — no debería ocurrir contra un
  /// servidor NexusCloud real, pero cubre el caso con seguridad.
  ///
  /// [statusCode] permite a quien llama ramificar por status HTTP incluso
  /// aquí -- caso real: una petición `dio.download()` (`ResponseType.
  /// stream`, ADR-010) nunca llega a tener `code` reconocible, porque
  /// Dio solo re-parsea un cuerpo de error a JSON DENTRO de su propio
  /// `download()`, después de que el interceptor de [ApiClient] ya haya
  /// decidido este `ApiException` sobre el stream crudo -- `statusCode`
  /// sigue siendo fiable en ese caso porque viene de la respuesta HTTP en
  /// sí, no de un cuerpo que aún no se ha podido leer.
  factory ApiException.unknown([
    String message = 'No se pudo completar la operación.',
    int? statusCode,
  ]) =>
      ApiException(code: 'unknown_error', message: message, statusCode: statusCode);

  @override
  String toString() => 'ApiException($code: $message)';
}
