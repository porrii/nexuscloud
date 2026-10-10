import 'dart:async';
import 'dart:collection';
import 'dart:typed_data';

import 'package:dio/dio.dart';

import '../../../core/network/api_client.dart';
import '../../../core/network/api_exception.dart';

/// Tipos para los que el servidor puede generar miniatura -- los mismos que
/// `ThumbnailKindForMimeType` en `internal/storage/thumbnail_job.go` y que
/// `hasThumbnail` en `web/src/components/FileThumbnail.tsx`: pedir la de
/// cualquier otro tipo es un 404 seguro.
bool mimeTypeHasThumbnail(String? mimeType) {
  final mime = (mimeType ?? '').toLowerCase();
  return mime.startsWith('image/') ||
      mime.startsWith('video/') ||
      mime == 'application/pdf';
}

/// Miniaturas del servidor (`GET /files/{id}/thumbnail`, §34, ADR-041):
/// JPEG de 320 px como máximo, generado bajo demanda.
///
/// No se puede usar `Image.network` porque la petición necesita el Bearer
/// del [ApiClient]; por eso los bytes se piden aquí y se pintan con
/// `Image.memory`. Además:
///  * **Caché en memoria** (LRU acotada por bytes), con clave `id` + SHA-256
///    del contenido: si el archivo cambia, su miniatura se vuelve a pedir.
///    Nunca en disco -- son derivados de archivos privados.
///  * **404 recordado** (miniaturas desactivadas en el servidor o formato que
///    no se puede decodificar): no se vuelve a pedir en esta sesión.
///  * **503 y errores de red NO se recuerdan**: son contención transitoria
///    (Decisión 6 del ADR), se reintentará la próxima vez que se muestre.
///  * **Concurrencia limitada** ([maxConcurrent]): una carpeta con cientos de
///    fotos no lanza cientos de peticiones a la vez; lo que deja de verse
///    antes de su turno se descarta sin llegar a pedirse.
class ThumbnailService {
  ThumbnailService({
    required ApiClient apiClient,
    this.maxConcurrent = 4,
    this.maxCacheBytes = 48 * 1024 * 1024,
  }) : _apiClient = apiClient;

  final ApiClient _apiClient;
  final int maxConcurrent;
  final int maxCacheBytes;

  // Un Map literal de Dart conserva el orden de inserción: el primero es el
  // menos reciente.
  final _cache = <String, Uint8List>{};
  final _missing = <String>{};
  final _waiters = Queue<Completer<void>>();
  int _cacheBytes = 0;
  int _running = 0;

  /// Se incrementa en [clear] para que una petición en vuelo de la sesión
  /// anterior no repueble la caché al terminar.
  int _epoch = 0;

  static String _key(String fileId, String sha256) => '$fileId:$sha256';

  /// Miniatura ya descargada, sin esperar (para pintarla en el primer
  /// frame y evitar el parpadeo al volver a una carpeta).
  Uint8List? cached(String fileId, String sha256) {
    final key = _key(fileId, sha256);
    final bytes = _cache.remove(key);
    if (bytes != null) _cache[key] = bytes; // la marca como reciente
    return bytes;
  }

  /// `true` si ya se sabe que ese archivo no tiene miniatura.
  bool isKnownMissing(String fileId, String sha256) =>
      _missing.contains(_key(fileId, sha256));

  /// Devuelve la miniatura o `null` si no hay (o no se pudo obtener ahora).
  /// Nunca lanza: una miniatura es decorativa, su fallo se ve como el icono
  /// del tipo de archivo. [stillWanted] se consulta al llegar el turno: si
  /// devuelve `false` (el elemento ya no está en pantalla) no se pide nada.
  Future<Uint8List?> load(
    String fileId,
    String sha256, {
    bool Function()? stillWanted,
  }) async {
    final key = _key(fileId, sha256);
    final hit = cached(fileId, sha256);
    if (hit != null) return hit;
    if (_missing.contains(key)) return null;

    await _acquire();
    try {
      // Mientras esperaba turno, otra petición pudo resolver la misma.
      final hitAfterWait = cached(fileId, sha256);
      if (hitAfterWait != null) return hitAfterWait;
      if (_missing.contains(key)) return null;
      if (stillWanted != null && !stillWanted()) return null;

      final epoch = _epoch;
      try {
        final response = await _apiClient.request(
          (dio) => dio.get<List<int>>(
            '/files/$fileId/thumbnail',
            options: Options(responseType: ResponseType.bytes),
          ),
        );
        final data = response.data;
        if (data == null || data.isEmpty) return null;
        final bytes = data is Uint8List ? data : Uint8List.fromList(data);
        if (epoch == _epoch) _put(key, bytes);
        return bytes;
      } on ApiException catch (e) {
        if (e.statusCode == 404 && epoch == _epoch) _missing.add(key);
        return null;
      }
    } finally {
      _release();
    }
  }

  /// Vacía todo (al cerrar sesión: son derivados de archivos privados).
  void clear() {
    _epoch++;
    _cache.clear();
    _missing.clear();
    _cacheBytes = 0;
  }

  void _put(String key, Uint8List bytes) {
    // Una sola miniatura mayor que toda la caché no se guarda.
    if (bytes.length > maxCacheBytes) return;
    final previous = _cache.remove(key);
    if (previous != null) _cacheBytes -= previous.length;
    _cache[key] = bytes;
    _cacheBytes += bytes.length;
    while (_cacheBytes > maxCacheBytes && _cache.isNotEmpty) {
      final oldest = _cache.keys.first;
      _cacheBytes -= _cache.remove(oldest)!.length;
    }
  }

  Future<void> _acquire() async {
    if (_running < maxConcurrent) {
      _running++;
      return;
    }
    final waiter = Completer<void>();
    _waiters.add(waiter);
    // El hueco lo transfiere _release directamente: _running no baja.
    await waiter.future;
  }

  void _release() {
    if (_waiters.isNotEmpty) {
      _waiters.removeFirst().complete();
    } else {
      _running--;
    }
  }
}
