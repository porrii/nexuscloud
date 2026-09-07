import '../../../../core/network/api_client.dart';
import '../../domain/entities/group.dart';
import '../../domain/entities/share.dart';
import '../models/group_model.dart';
import '../models/share_model.dart';

class SharingRemoteDataSource {
  SharingRemoteDataSource({required ApiClient apiClient})
      : _apiClient = apiClient;

  final ApiClient _apiClient;

  /// Igual que `/files/{id}/versions`: el servidor devuelve un array JSON
  /// crudo, no un objeto con clave.
  Future<List<Group>> listGroups() async {
    final response =
        await _apiClient.request((dio) => dio.get<List<dynamic>>('/groups'));
    return (response.data ?? [])
        .cast<Map<String, dynamic>>()
        .map(GroupModel.fromJson)
        .toList();
  }

  /// Mismo patrón de `POST` con cuerpo JSON tipo objeto que ya usa
  /// `AuthRemoteDataSource.login()` -- el interceptor por defecto de
  /// `dio` detecta un `Map` y pone `application/json` solo, sin
  /// configuración adicional.
  ///
  /// `expiresAt` se serializa con `.toUtc()` antes de `toIso8601String()`
  /// -- sin eso, el resultado no lleva marca de zona horaria y el
  /// servidor (que exige RFC3339 estricto) rechaza la petición entera con
  /// `400 invalid_request`.
  Future<Share> createShare({
    required ShareResourceType resourceType,
    required String resourceId,
    required ShareType shareType,
    String? targetUsername,
    String? targetGroupId,
    String? label,
    bool? canDownload,
    bool canUpload = false,
    String? password,
    DateTime? expiresAt,
    int? maxDownloads,
  }) async {
    final response = await _apiClient.request(
      (dio) => dio.post<Map<String, dynamic>>(
        '/shares',
        data: {
          'resource_type': resourceType.wireValue,
          'resource_id': resourceId,
          'share_type': shareType.wireValue,
          if (targetUsername != null && targetUsername.isNotEmpty)
            'target_username': targetUsername,
          'target_group_id': ?targetGroupId,
          if (label != null && label.isNotEmpty) 'label': label,
          'can_download': ?canDownload,
          'can_upload': canUpload,
          if (password != null && password.isNotEmpty) 'password': password,
          if (expiresAt != null)
            'expires_at': expiresAt.toUtc().toIso8601String(),
          'max_downloads': ?maxDownloads,
        },
      ),
    );
    return ShareModel.fromJson(response.data!);
  }

  Future<List<Share>> listShares({required ShareDirection direction}) async {
    final response = await _apiClient.request(
      (dio) => dio.get<List<dynamic>>(
        '/shares',
        queryParameters: {'direction': direction.wireValue},
      ),
    );
    return (response.data ?? [])
        .cast<Map<String, dynamic>>()
        .map(ShareModel.fromJson)
        .toList();
  }

  Future<void> revokeShare(String shareId) {
    return _apiClient.request((dio) => dio.delete<void>('/shares/$shareId'));
  }
}
