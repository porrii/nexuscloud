import '../../../core/network/api_client.dart';
import '../../../core/network/api_exception.dart';
import '../../files/data/models/directory_entry_model.dart';
import '../../files/data/models/file_entry_model.dart';
import '../../files/domain/entities/directory_listing.dart';

/// Filtro de tipo ofrecido en la pantalla de búsqueda -- se traduce al
/// parámetro `type` (prefijo MIME) o `ext` del servidor.
enum SearchKind {
  all('Todo'),
  images('Imágenes'),
  videos('Vídeos'),
  audio('Audio'),
  pdf('PDF');

  const SearchKind(this.label);
  final String label;

  Map<String, String> get queryParameters => switch (this) {
    SearchKind.all => const {},
    SearchKind.images => const {'type': 'image/'},
    SearchKind.videos => const {'type': 'video/'},
    SearchKind.audio => const {'type': 'audio/'},
    SearchKind.pdf => const {'ext': 'pdf'},
  };
}

/// Búsqueda de metadatos sobre todo el árbol propio (`GET /search`, §33,
/// ADR-040). Misma forma de respuesta que `GET /files`, así que se
/// reutilizan los modelos del listado.
class SearchService {
  SearchService({required ApiClient apiClient}) : _apiClient = apiClient;

  final ApiClient _apiClient;

  /// Lanza [ApiException]; un `404` significa que la instancia tiene la
  /// búsqueda desactivada (`search.enabled=false`).
  Future<DirectoryListing> search(
    String query, {
    SearchKind kind = SearchKind.all,
  }) async {
    final response = await _apiClient.request(
      (dio) => dio.get<Map<String, dynamic>>(
        '/search',
        queryParameters: {
          if (query.trim().isNotEmpty) 'q': query.trim(),
          ...kind.queryParameters,
        },
      ),
    );
    final data = response.data ?? const <String, dynamic>{};
    final directories = (data['directories'] as List? ?? const [])
        .cast<Map<String, dynamic>>()
        .map(DirectoryEntryModel.fromJson)
        .toList();
    final files = (data['files'] as List? ?? const [])
        .cast<Map<String, dynamic>>()
        .map(FileEntryModel.fromJson)
        .toList();
    return DirectoryListing(directories: directories, files: files);
  }
}
