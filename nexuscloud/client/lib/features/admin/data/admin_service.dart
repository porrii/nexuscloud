import '../../../core/network/api_client.dart';
import '../../../core/network/api_exception.dart';
import 'admin_models.dart';

/// Llamadas de administración de la API v1 (todas exigen rol de
/// administrador en el servidor: `RequireAdmin`). Salvo [isAdmin], todas
/// lanzan [ApiException]; la pantalla muestra su mensaje, que el servidor
/// ya redacta para el usuario.
///
/// La interfaz del cliente solo decide qué se ENSEÑA: quien decide qué se
/// PUEDE hacer es siempre el servidor.
class AdminService {
  AdminService({required ApiClient apiClient}) : _apiClient = apiClient;

  final ApiClient _apiClient;

  /// `GET /users/me` → `is_admin`. Silencioso: ante un fallo devuelve
  /// `null` («no se sabe»), para que quien llama lo vuelva a intentar en
  /// vez de dar por hecho que no es administrador.
  Future<bool?> isAdmin() async {
    try {
      final response = await _apiClient.request(
        (dio) => dio.get<Map<String, dynamic>>('/users/me'),
      );
      return response.data?['is_admin'] == true;
    } on ApiException {
      return null;
    }
  }

  Future<List<Map<String, dynamic>>> _getList(String path) async {
    final response = await _apiClient.request(
      (dio) => dio.get<List<dynamic>>(path),
    );
    return (response.data ?? const [])
        .whereType<Map>()
        .map((e) => e.cast<String, dynamic>())
        .toList();
  }

  // --- Usuarios ------------------------------------------------------------

  Future<List<AdminUser>> listUsers() async =>
      (await _getList('/users')).map(AdminUser.fromJson).toList();

  Future<AdminUser> createUser({
    required String username,
    required String password,
    required UserRole role,
    String? displayName,
    String? email,
    QuotaBytes quotaBytes,
    bool setQuota = false,
  }) async {
    final response = await _apiClient.request(
      (dio) => dio.post<Map<String, dynamic>>(
        '/users',
        data: {
          'username': username,
          'password': password,
          'role': role.id,
          if (displayName != null && displayName.isNotEmpty)
            'display_name': displayName,
          if (email != null && email.isNotEmpty) 'email': email,
          if (setQuota) 'quota_bytes': quotaBytes,
        },
      ),
    );
    return AdminUser.fromJson(response.data!);
  }

  /// Solo se envía lo que cambia. [setQuota] distingue «no tocar la cuota»
  /// de «dejarla en null (heredar)».
  Future<AdminUser> updateUser(
    String id, {
    String? displayName,
    String? email,
    bool? active,
    QuotaBytes quotaBytes,
    bool setQuota = false,
  }) async {
    final response = await _apiClient.request(
      (dio) => dio.patch<Map<String, dynamic>>(
        '/users/$id',
        data: {
          'display_name': ?displayName,
          'email': ?email,
          if (active != null) 'status': active ? 'active' : 'disabled',
          if (setQuota) 'quota_bytes': quotaBytes,
        },
      ),
    );
    return AdminUser.fromJson(response.data!);
  }

  Future<void> deleteUser(String id) async {
    await _apiClient.request((dio) => dio.delete<void>('/users/$id'));
  }

  // --- Grupos --------------------------------------------------------------

  Future<List<AdminGroup>> listGroups() async =>
      (await _getList('/groups')).map(AdminGroup.fromJson).toList();

  Future<AdminGroup> createGroup(String name, {QuotaBytes quotaBytes}) async {
    final response = await _apiClient.request(
      (dio) => dio.post<Map<String, dynamic>>(
        '/groups',
        data: {'name': name, 'quota_bytes': quotaBytes},
      ),
    );
    return AdminGroup.fromJson(response.data!);
  }

  Future<AdminGroup> updateGroupQuota(String id, QuotaBytes quotaBytes) async {
    final response = await _apiClient.request(
      (dio) => dio.patch<Map<String, dynamic>>(
        '/groups/$id',
        data: {'quota_bytes': quotaBytes},
      ),
    );
    return AdminGroup.fromJson(response.data!);
  }

  Future<void> addGroupMember(String groupId, String userId) async {
    await _apiClient.request(
      (dio) =>
          dio.post<void>('/groups/$groupId/members', data: {'user_id': userId}),
    );
  }

  // --- Invitaciones ----------------------------------------------------------

  Future<List<AdminInvitation>> listInvitations() async =>
      (await _getList('/invitations')).map(AdminInvitation.fromJson).toList();

  Future<CreatedInvitation> createInvitation({
    required UserRole role,
    required int maxUses,
    required int ttlHours,
  }) async {
    final response = await _apiClient.request(
      (dio) => dio.post<Map<String, dynamic>>(
        '/invitations',
        data: {'role': role.id, 'max_uses': maxUses, 'ttl_hours': ttlHours},
      ),
    );
    final data = response.data!;
    return CreatedInvitation(
      invitation: AdminInvitation.fromJson(
        (data['invitation'] as Map).cast<String, dynamic>(),
      ),
      token: data['token'] as String,
    );
  }

  Future<void> revokeInvitation(String id) async {
    await _apiClient.request((dio) => dio.delete<void>('/invitations/$id'));
  }

  // --- Auditoría y sistema ---------------------------------------------------

  Future<List<AuditEvent>> listAuditEvents({
    int limit = 100,
    int offset = 0,
  }) async =>
      (await _getList('/audit?limit=$limit&offset=$offset'))
          .map(AuditEvent.fromJson)
          .toList();

  /// `null` si el sistema operativo del servidor no permite enumerarlos.
  Future<List<DiskInfo>?> listDisks() async {
    try {
      return (await _getList('/storage/disks')).map(DiskInfo.fromJson).toList();
    } on ApiException catch (e) {
      if (e.code == 'not_supported') return null;
      rethrow;
    }
  }

  Future<List<ThumbnailJob>> listThumbnailJobs({required bool failed}) async =>
      (await _getList(
        '/admin/thumbnail-jobs?status=${failed ? 'failed' : 'pending'}',
      )).map(ThumbnailJob.fromJson).toList();
}
