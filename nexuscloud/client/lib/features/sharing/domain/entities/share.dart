import 'package:equatable/equatable.dart';

/// Tipo de recurso compartido -- solo archivos y carpetas tienen concepto
/// de compartición en el servidor (ADR-008).
enum ShareResourceType { file, directory }

/// A qué se concede acceso: un usuario concreto (por nombre exacto), un
/// grupo, o un enlace público (opcionalmente con contraseña/expiración/
/// límite de descargas).
enum ShareType { user, group, link }

/// Dirección de `GET /shares?direction=...`. Este slice solo dispara
/// [byMe] (gestionar lo que YO comparto); [withMe] ya está modelado
/// porque el endpoint lo soporta 1:1, así que un slice futuro de
/// "Compartido conmigo" no necesita tocar el repositorio, solo añadir una
/// página nueva que lo use.
enum ShareDirection { byMe, withMe }

extension ShareResourceTypeWire on ShareResourceType {
  String get wireValue => switch (this) {
        ShareResourceType.file => 'file',
        ShareResourceType.directory => 'directory',
      };

  static ShareResourceType fromWire(String value) => switch (value) {
        'file' => ShareResourceType.file,
        'directory' => ShareResourceType.directory,
        _ => throw ArgumentError('resource_type desconocido: $value'),
      };
}

extension ShareTypeWire on ShareType {
  String get wireValue => switch (this) {
        ShareType.user => 'user',
        ShareType.group => 'group',
        ShareType.link => 'link',
      };

  static ShareType fromWire(String value) => switch (value) {
        'user' => ShareType.user,
        'group' => ShareType.group,
        'link' => ShareType.link,
        _ => throw ArgumentError('share_type desconocido: $value'),
      };
}

extension ShareDirectionWire on ShareDirection {
  String get wireValue => switch (this) {
        ShareDirection.byMe => 'by-me',
        ShareDirection.withMe => 'with-me',
      };
}

/// Una compartición (ADR-008): concede acceso de descarga (y, solo para
/// enlaces sobre una carpeta, también de subida) a un recurso propio. No
/// hay endpoint de "actualizar" -- cambiar algo implica revocar y crear
/// una nueva (para un enlace, con un token distinto).
class Share extends Equatable {
  const Share({
    required this.id,
    required this.resourceType,
    required this.resourceId,
    this.resourceName,
    required this.shareType,
    this.targetUserId,
    this.targetUsername,
    this.targetGroupId,
    this.targetGroupName,
    this.label,
    required this.canDownload,
    required this.canUpload,
    required this.hasPassword,
    this.expiresAt,
    this.maxDownloads,
    required this.downloadCount,
    required this.createdAt,
    this.token,
  });

  final String id;
  final ShareResourceType resourceType;
  final String resourceId;
  final String? resourceName;
  final ShareType shareType;
  final String? targetUserId;
  final String? targetUsername;
  final String? targetGroupId;
  final String? targetGroupName;
  final String? label;
  final bool canDownload;
  final bool canUpload;
  final bool hasPassword;
  final DateTime? expiresAt;
  final int? maxDownloads;
  final int downloadCount;
  final DateTime createdAt;

  /// Token en claro del enlace público -- el servidor solo lo manda una
  /// vez, en la respuesta de creación. Ausente en cualquier otra
  /// respuesta (incluida `listShares`), y ausente siempre salvo para
  /// `shareType == ShareType.link`.
  final String? token;

  @override
  List<Object?> get props => [
        id,
        resourceType,
        resourceId,
        resourceName,
        shareType,
        targetUserId,
        targetUsername,
        targetGroupId,
        targetGroupName,
        label,
        canDownload,
        canUpload,
        hasPassword,
        expiresAt,
        maxDownloads,
        downloadCount,
        createdAt,
        token,
      ];
}
