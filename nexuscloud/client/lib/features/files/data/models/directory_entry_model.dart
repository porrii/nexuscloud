import '../../domain/entities/directory_entry.dart';

class DirectoryEntryModel {
  const DirectoryEntryModel._();

  static DirectoryEntry fromJson(Map<String, dynamic> json) => DirectoryEntry(
        id: json['id'] as String,
        parentPath: json['parent_path'] as String,
        name: json['name'] as String,
        createdAt: DateTime.parse(json['created_at'] as String),
        deletedAt: json['deleted_at'] != null
            ? DateTime.parse(json['deleted_at'] as String)
            : null,
      );
}
