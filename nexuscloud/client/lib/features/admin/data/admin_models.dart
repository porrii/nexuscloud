/// Modelos de la API de administración (`RequireAdmin` en
/// `internal/api/v1/router.go`). Solo lectura de JSON: el cliente no
/// guarda nada de esto en disco.
library;

DateTime? _date(Object? value) =>
    value is String ? DateTime.tryParse(value)?.toLocal() : null;

int? _int(Object? value) => (value as num?)?.toInt();

/// Cuota propia de un usuario o por miembro de un grupo (§24, ADR-036):
/// `null` = hereda (usuario) o no aporta (grupo), `0` = ilimitada, `>0` =
/// límite en bytes. Se envía tal cual en `quota_bytes`.
typedef QuotaBytes = int?;

enum UserRole {
  superAdmin('super_admin', 'Superadministrador'),
  administrator('administrator', 'Administrador'),
  user('user', 'Usuario'),
  readOnly('read_only', 'Solo lectura');

  const UserRole(this.id, this.label);

  final String id;
  final String label;

  /// Roles que se pueden asignar desde la app. «Solo lectura» existe en el
  /// servidor (`users.RoleReadOnly`) pero ninguna ruta lo aplica todavía:
  /// ofrecerlo haría creer que esa cuenta no puede subir ni borrar, y sí
  /// puede. Se seguirá mostrando si llega en una invitación ya creada.
  static List<UserRole> get assignable =>
      values.where((r) => r != readOnly).toList();
}

class AdminUser {
  const AdminUser({
    required this.id,
    required this.username,
    required this.displayName,
    required this.active,
    required this.hasTotp,
    required this.createdAt,
    this.email,
    this.lastLoginAt,
    this.quotaBytes,
  });

  factory AdminUser.fromJson(Map<String, dynamic> json) => AdminUser(
    id: json['id'] as String,
    username: json['username'] as String? ?? '',
    displayName: json['display_name'] as String? ?? '',
    email: (json['email'] as String?)?.isEmpty ?? true
        ? null
        : json['email'] as String,
    active: json['status'] != 'disabled',
    hasTotp: json['has_totp'] as bool? ?? false,
    createdAt: _date(json['created_at']) ?? DateTime.now(),
    lastLoginAt: _date(json['last_login_at']),
    quotaBytes: _int(json['quota_bytes']),
  );

  final String id;
  final String username;
  final String displayName;
  final String? email;
  final bool active;
  final bool hasTotp;
  final DateTime createdAt;
  final DateTime? lastLoginAt;
  final QuotaBytes quotaBytes;

  String get label => displayName.isEmpty ? username : displayName;
}

class AdminGroup {
  const AdminGroup({required this.id, required this.name, this.quotaBytes});

  factory AdminGroup.fromJson(Map<String, dynamic> json) => AdminGroup(
    id: json['id'] as String,
    name: json['name'] as String? ?? '',
    quotaBytes: _int(json['quota_bytes']),
  );

  final String id;
  final String name;
  final QuotaBytes quotaBytes;
}

class AdminInvitation {
  const AdminInvitation({
    required this.id,
    required this.maxUses,
    required this.useCount,
    required this.expiresAt,
    required this.createdAt,
    required this.revoked,
    this.roleId,
  });

  factory AdminInvitation.fromJson(Map<String, dynamic> json) =>
      AdminInvitation(
        id: json['id'] as String,
        roleId: (json['role_id'] as String?)?.isEmpty ?? true
            ? null
            : json['role_id'] as String,
        maxUses: _int(json['max_uses']) ?? 1,
        useCount: _int(json['use_count']) ?? 0,
        expiresAt: _date(json['expires_at']) ?? DateTime.now(),
        createdAt: _date(json['created_at']) ?? DateTime.now(),
        revoked: json['revoked'] as bool? ?? false,
      );

  final String id;
  final String? roleId;
  final int maxUses;
  final int useCount;
  final DateTime expiresAt;
  final DateTime createdAt;
  final bool revoked;

  bool get expired => expiresAt.isBefore(DateTime.now());
  bool get exhausted => useCount >= maxUses;
  bool get usable => !revoked && !expired && !exhausted;
}

/// Invitación recién creada: el token solo se ve esta vez (§78).
class CreatedInvitation {
  const CreatedInvitation({required this.invitation, required this.token});

  final AdminInvitation invitation;
  final String token;
}

/// Evento de auditoría. Desde ADR-042 (Decisión 7) el servidor usa claves
/// snake_case (`id`, `occurred_at`...); los servidores anteriores mandaban
/// los nombres de campo de Go (`ID`, `OccurredAt`...), que se siguen
/// aceptando como respaldo.
class AuditEvent {
  const AuditEvent({
    required this.id,
    required this.occurredAt,
    required this.eventType,
    this.actorUserId,
    this.targetType,
    this.targetId,
    this.ip,
    this.metadata = const {},
  });

  factory AuditEvent.fromJson(Map<String, dynamic> json) {
    // Clave nueva (snake_case) o, si no está, la antigua de Go.
    Object? field(String key, String legacyKey) => json[key] ?? json[legacyKey];
    String? str(String key, String legacyKey) {
      final value = field(key, legacyKey) as String?;
      return value == null || value.isEmpty ? null : value;
    }

    return AuditEvent(
      id: str('id', 'ID') ?? '',
      occurredAt: _date(field('occurred_at', 'OccurredAt')) ?? DateTime.now(),
      eventType: str('event_type', 'EventType') ?? '',
      actorUserId: str('actor_user_id', 'ActorUserID'),
      targetType: str('target_type', 'TargetType'),
      targetId: str('target_id', 'TargetID'),
      ip: str('ip', 'IP'),
      metadata:
          (field('metadata', 'Metadata') as Map?)?.cast<String, dynamic>() ??
          const {},
    );
  }

  final String id;
  final DateTime occurredAt;
  final String eventType;
  final String? actorUserId;
  final String? targetType;
  final String? targetId;
  final String? ip;
  final Map<String, dynamic> metadata;
}

class DiskInfo {
  const DiskInfo({
    required this.mountPoint,
    required this.totalBytes,
    required this.freeBytes,
    required this.usedBytes,
    required this.readOnly,
    this.filesystem,
    this.device,
    this.label,
  });

  factory DiskInfo.fromJson(Map<String, dynamic> json) => DiskInfo(
    mountPoint: json['mount_point'] as String? ?? '',
    filesystem: json['filesystem'] as String?,
    device: json['device'] as String?,
    label: json['label'] as String?,
    totalBytes: _int(json['total_bytes']) ?? 0,
    freeBytes: _int(json['free_bytes']) ?? 0,
    usedBytes: _int(json['used_bytes']) ?? 0,
    readOnly: json['read_only'] as bool? ?? false,
  );

  final String mountPoint;
  final String? filesystem;
  final String? device;
  final String? label;
  final int totalBytes;
  final int freeBytes;
  final int usedBytes;
  final bool readOnly;

  double get fraction =>
      totalBytes <= 0 ? 0 : (usedBytes / totalBytes).clamp(0, 1).toDouble();
}

class ThumbnailJob {
  const ThumbnailJob({
    required this.id,
    required this.fileId,
    required this.kind,
    required this.status,
    required this.attempts,
    required this.updatedAt,
    this.lastError,
  });

  factory ThumbnailJob.fromJson(Map<String, dynamic> json) => ThumbnailJob(
    id: json['id'] as String,
    fileId: json['file_id'] as String? ?? '',
    kind: json['kind'] as String? ?? '',
    status: json['status'] as String? ?? '',
    attempts: _int(json['attempts']) ?? 0,
    lastError: json['last_error'] as String?,
    updatedAt: _date(json['updated_at']) ?? DateTime.now(),
  );

  final String id;
  final String fileId;
  final String kind;
  final String status;
  final int attempts;
  final String? lastError;
  final DateTime updatedAt;
}
