import 'dart:typed_data';

import 'package:dio/dio.dart';

import '../../../core/network/api_client.dart';
import '../../../core/network/api_exception.dart';

/// Contenido de un archivo para la vista previa (§35): en memoria para
/// imagen, PDF y texto, o como fuente autenticada para que el reproductor
/// lo pida por partes (vídeo y audio). Mismo `GET /files/{id}` que la
/// descarga, pero sin pasar por disco ni por la cola de transferencias.
class FileContentService {
  FileContentService({required ApiClient apiClient}) : _apiClient = apiClient;

  final ApiClient _apiClient;

  /// Lanza [ApiException] si el servidor lo rechaza o no hay red.
  ///
  /// [maxBytes] corta la descarga en cuanto la supera (código `too_large`):
  /// el límite se decide con el tamaño del listado, pero el archivo pudo
  /// cambiar después y no se debe cargar en memoria sin tope.
  Future<Uint8List> fetchBytes(String fileId, {int? maxBytes}) async {
    final cancel = CancelToken();
    var tooLarge = false;
    try {
      final response = await _apiClient.request(
        (dio) => dio.get<List<int>>(
          '/files/$fileId',
          cancelToken: cancel,
          onReceiveProgress: maxBytes == null
              ? null
              : (received, _) {
                  if (received > maxBytes && !tooLarge) {
                    tooLarge = true;
                    cancel.cancel();
                  }
                },
          options: Options(
            responseType: ResponseType.bytes,
            // Un PDF grande puede tardar más que las respuestas JSON.
            receiveTimeout: const Duration(minutes: 2),
          ),
        ),
      );
      final data = response.data ?? const <int>[];
      return data is Uint8List ? data : Uint8List.fromList(data);
    } on ApiException {
      if (tooLarge) {
        throw const ApiException(
          code: 'too_large',
          message: 'El archivo es demasiado grande para la vista previa.',
        );
      }
      rethrow;
    }
  }

  /// Los primeros [length] bytes, para comprobar qué es realmente un
  /// archivo antes de dárselo al reproductor. Se lee en streaming y se
  /// cancela en cuanto llegan: el servidor solo entiende rangos `bytes=N-`
  /// (reanudación), así que pedir `bytes=0-1023` devolvería el archivo
  /// entero. Cancelar cierra el socket (verificado: se transfieren unos
  /// cientos de KB de un archivo de 50 MB, lo que quepa en los búferes).
  Future<Uint8List> fetchPrefix(String fileId, {int length = 1024}) async {
    final cancel = CancelToken();
    final response = await _apiClient.request(
      (dio) => dio.get<ResponseBody>(
        '/files/$fileId',
        options: Options(responseType: ResponseType.stream),
        cancelToken: cancel,
      ),
    );
    final out = BytesBuilder(copy: false);
    try {
      await for (final chunk in response.data!.stream) {
        out.add(chunk);
        if (out.length >= length) break;
      }
    } finally {
      cancel.cancel();
    }
    final bytes = out.takeBytes();
    return bytes.length > length
        ? Uint8List.sublistView(bytes, 0, length)
        : bytes;
  }

  Future<({Uri uri, Map<String, String> headers})> streamSource(
    String fileId,
  ) => _apiClient.authorizedResource('/files/$fileId');
}
