import '../../domain/entities/file_entry.dart';

class FileEntryModel {
  const FileEntryModel._();

  static FileEntry fromJson(Map<String, dynamic> json) => FileEntry(
        id: json['id'] as String,
        parentPath: json['parent_path'] as String,
        name: json['name'] as String,
        sizeBytes: json['size_bytes'] as int,
        sha256: json['sha256'] as String,
        mimeType: json['mime_type'] as String,
        createdAt: DateTime.parse(json['created_at'] as String),
        updatedAt: DateTime.parse(json['updated_at'] as String),
      );
}
