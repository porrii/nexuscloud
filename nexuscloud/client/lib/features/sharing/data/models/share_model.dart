import '../../domain/entities/share.dart';

class ShareModel {
  const ShareModel._();

  static Share fromJson(Map<String, dynamic> json) => Share(
        id: json['id'] as String,
        resourceType:
            ShareResourceTypeWire.fromWire(json['resource_type'] as String),
        resourceId: json['resource_id'] as String,
        resourceName: json['resource_name'] as String?,
        shareType: ShareTypeWire.fromWire(json['share_type'] as String),
        targetUserId: json['target_user_id'] as String?,
        targetUsername: json['target_username'] as String?,
        targetGroupId: json['target_group_id'] as String?,
        targetGroupName: json['target_group_name'] as String?,
        label: json['label'] as String?,
        canDownload: json['can_download'] as bool,
        canUpload: json['can_upload'] as bool,
        hasPassword: json['has_password'] as bool,
        expiresAt: json['expires_at'] != null
            ? DateTime.parse(json['expires_at'] as String)
            : null,
        maxDownloads: json['max_downloads'] as int?,
        downloadCount: json['download_count'] as int,
        createdAt: DateTime.parse(json['created_at'] as String),
        token: json['token'] as String?,
      );
}
