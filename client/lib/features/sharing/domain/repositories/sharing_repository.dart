import '../entities/group.dart';
import '../entities/share.dart';

abstract interface class SharingRepository {
  Future<List<Group>> listGroups();

  /// Crea una compartición. `canUpload` solo tiene efecto real cuando
  /// [resourceType] es [ShareResourceType.directory] y [shareType] es
  /// [ShareType.link] -- el servidor lo fuerza a `false` en cualquier
  /// otra combinación, sin aviso. `password`/`expiresAt`/`maxDownloads`
  /// solo tienen sentido para [ShareType.link] (el servidor los ignora en
  /// user/group, pero no hace falta que el cliente lo replique: no se
  /// muestran esos campos fuera de la pestaña Enlace).
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
  });

  /// El servidor no filtra por recurso -- para "¿quién tiene acceso a
  /// este archivo?" hay que pedir la lista completa y filtrar en el
  /// cliente por `resourceId`.
  Future<List<Share>> listShares({required ShareDirection direction});

  /// Revocación irreversible (soft-update sin endpoint de "des-revocar").
  Future<void> revokeShare(String shareId);

  /// URL base del servidor tal como el usuario la escribió en el login
  /// (sin el sufijo `/api/v1` que sí lleva la baseUrl interna de `Dio`).
  /// Se usa para construir la URL pública mostrable
  /// `{serverBaseUrl}/s/{token}` tras crear un enlace.
  Future<String?> get serverBaseUrl;
}
