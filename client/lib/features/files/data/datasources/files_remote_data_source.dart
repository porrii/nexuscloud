import '../../../../core/network/api_client.dart';
import '../../domain/entities/directory_listing.dart';
import '../models/directory_entry_model.dart';
import '../models/file_entry_model.dart';

class FilesRemoteDataSource {
  FilesRemoteDataSource({required ApiClient apiClient}) : _apiClient = apiClient;

  final ApiClient _apiClient;

  Future<DirectoryListing> list(String path) async {
    final response = await _apiClient.request(
      (dio) => dio.get<Map<String, dynamic>>(
        '/files',
        queryParameters: {'path': path},
      ),
    );
    final data = response.data!;
    final directories = (data['directories'] as List)
        .cast<Map<String, dynamic>>()
        .map(DirectoryEntryModel.fromJson)
        .toList();
    final files = (data['files'] as List)
        .cast<Map<String, dynamic>>()
        .map(FileEntryModel.fromJson)
        .toList();
    return DirectoryListing(directories: directories, files: files);
  }
}
