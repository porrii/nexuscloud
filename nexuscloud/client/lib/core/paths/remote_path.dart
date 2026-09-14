import 'package:path/path.dart' as p;

/// Utilidades para el espacio de rutas virtual que define el servidor
/// (`parent_path`/`path` en la API REST) — SIEMPRE con `/`, sea cual sea el
/// SO del cliente.
///
/// Usa explícitamente `path.posix` en vez del contexto por defecto de
/// `package:path` (que en Windows resolvería con `\`), porque esto no es
/// una ruta del filesystem local: es un identificador lógico que el
/// servidor entiende tal cual (`internal/api/v1/files_handlers.go`).
/// §180 pide no construir rutas concatenando strings a mano — aun así, el
/// join aquí es deliberadamente simple porque el espacio de nombres remoto
/// no tiene los casos raros de un filesystem real (unidades, symlinks,
/// rutas UNC); esos sí importan para §181/§182, pero se aplican al
/// filesystem LOCAL del cliente, no a esta ruta lógica remota.
class RemotePath {
  const RemotePath._();

  static const String root = '/';

  /// Une un directorio padre con un nombre de entrada.
  /// Ej.: join('/', 'Documentos') -> '/Documentos';
  ///      join('/Documentos', 'Fotos') -> '/Documentos/Fotos'.
  static String join(String parentPath, String name) {
    if (parentPath == root) return '$root$name';
    return '$parentPath/$name';
  }

  /// Segmentos no vacíos de una ruta, para construir breadcrumbs.
  /// Ej.: segments('/Documentos/Fotos') -> ['Documentos', 'Fotos'].
  static List<String> segments(String path) => p.posix
      .split(path)
      .where((s) => s.isNotEmpty && s != '/')
      .toList(growable: false);

  /// Reconstruye la ruta hasta (e incluyendo) el segmento en [index] de
  /// una lista ya obtenida con [segments] — para navegar al tocar un
  /// breadcrumb intermedio.
  static String pathUpTo(List<String> pathSegments, int index) {
    if (index < 0) return root;
    return '$root${pathSegments.sublist(0, index + 1).join('/')}';
  }
}
