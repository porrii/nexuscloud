import '../entities/directory_listing.dart';

/// Solo lectura en este slice -- deliberadamente NO tiene el patrón
/// Stream+getter de auth (ver `AuthRepository`): no hay mutación local ni
/// varios observadores, un simple `Future` de petición/respuesta es la
/// representación honesta (ADR-009).
abstract interface class FilesRepository {
  Future<DirectoryListing> list(String path);
}
