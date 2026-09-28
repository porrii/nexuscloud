import '../domain/entities/directory_entry.dart';
import '../domain/entities/directory_listing.dart';
import '../domain/entities/file_entry.dart';

/// Carpeta o archivo en la vista del explorador -- une las dos entidades
/// del dominio para que selección, orden y acciones se escriban una vez.
class BrowserItem {
  const BrowserItem.directory(DirectoryEntry this.directory) : file = null;

  const BrowserItem.file(FileEntry this.file) : directory = null;

  final DirectoryEntry? directory;
  final FileEntry? file;

  bool get isDirectory => directory != null;

  String get id => directory?.id ?? file!.id;

  String get name => directory?.name ?? file!.name;

  /// Clave única en la selección (una carpeta y un archivo podrían, en
  /// teoría, compartir id en backends distintos).
  String get key => isDirectory ? 'd:$id' : 'f:$id';

  int? get sizeBytes => file?.sizeBytes;

  String? get mimeType => file?.mimeType;

  /// Las carpetas no tienen `updatedAt` en la API; se usa su creación.
  DateTime get modifiedAt => file?.updatedAt ?? directory!.createdAt;
}

enum SortField { name, size, modified }

class SortSpec {
  const SortSpec(this.field, {this.ascending = true});

  final SortField field;
  final bool ascending;

  SortSpec toggle(SortField tapped) => tapped == field
      ? SortSpec(field, ascending: !ascending)
      // Tamaño y fecha empiezan descendentes (lo grande / lo reciente
      // primero), que es casi siempre lo que se busca al pulsarlas.
      : SortSpec(tapped, ascending: tapped == SortField.name);
}

/// Convierte el listado en elementos ordenados y filtrados. Las carpetas
/// van siempre primero, como en el Explorador de Windows y en la web.
List<BrowserItem> buildBrowserItems(
  DirectoryListing listing, {
  required SortSpec sort,
  String filter = '',
}) {
  final needle = filter.trim().toLowerCase();
  bool matches(String name) =>
      needle.isEmpty || name.toLowerCase().contains(needle);

  int compare(BrowserItem a, BrowserItem b) {
    final int result = switch (sort.field) {
      SortField.name => a.name.toLowerCase().compareTo(b.name.toLowerCase()),
      SortField.size => (a.sizeBytes ?? 0).compareTo(b.sizeBytes ?? 0),
      SortField.modified => a.modifiedAt.compareTo(b.modifiedAt),
    };
    final tieBroken = result != 0
        ? result
        : a.name.toLowerCase().compareTo(b.name.toLowerCase());
    return sort.ascending ? tieBroken : -tieBroken;
  }

  final directories = [
    for (final d in listing.directories)
      if (matches(d.name)) BrowserItem.directory(d),
  ]..sort(compare);
  final files = [
    for (final f in listing.files)
      if (matches(f.name)) BrowserItem.file(f),
  ]..sort(compare);
  return [...directories, ...files];
}
