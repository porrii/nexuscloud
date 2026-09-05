import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/network/api_exception.dart';
import 'package:nexuscloud_client/features/files/data/datasources/files_remote_data_source.dart';
import 'package:nexuscloud_client/features/files/data/repositories/files_repository_impl.dart';
import 'package:nexuscloud_client/features/files/domain/entities/directory_listing.dart';

class _FakeFilesRemoteDataSource implements FilesRemoteDataSource {
  final Map<String, DirectoryListing> listingsByPath = {};
  ApiException? errorToThrow;

  @override
  Future<DirectoryListing> list(String path) async {
    if (errorToThrow != null) throw errorToThrow!;
    return listingsByPath[path] ??
        const DirectoryListing(directories: [], files: []);
  }
}

void main() {
  test('list delega en el data source y devuelve tal cual el resultado', () async {
    final fake = _FakeFilesRemoteDataSource();
    const listing = DirectoryListing(directories: [], files: []);
    fake.listingsByPath['/Documentos'] = listing;
    final repo = FilesRepositoryImpl(remoteDataSource: fake);

    final result = await repo.list('/Documentos');

    expect(result, listing);
  });

  test('una ApiException del data source se propaga sin cambios', () async {
    final fake = _FakeFilesRemoteDataSource()
      ..errorToThrow = const ApiException(code: 'forbidden', message: 'x');
    final repo = FilesRepositoryImpl(remoteDataSource: fake);

    await expectLater(
      repo.list('/'),
      throwsA(isA<ApiException>().having((e) => e.code, 'code', 'forbidden')),
    );
  });
}
