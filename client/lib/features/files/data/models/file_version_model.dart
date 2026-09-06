import '../../domain/entities/file_version.dart';

class FileVersionModel {
  const FileVersionModel._();

  static FileVersion fromJson(Map<String, dynamic> json) => FileVersion(
        versionNum: json['version_num'] as int,
        sizeBytes: json['size_bytes'] as int,
        sha256: json['sha256'] as String,
        mimeType: json['mime_type'] as String,
        createdAt: DateTime.parse(json['created_at'] as String),
      );
}
