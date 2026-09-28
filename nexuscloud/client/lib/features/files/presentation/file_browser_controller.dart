/// Permite a otra sección (la búsqueda) llevar el explorador a una carpeta
/// y resaltar un elemento, sin que la barra lateral tenga que conocer el
/// estado interno de la página. Si el explorador aún no se ha construido,
/// la petición queda pendiente hasta que se registre.
class FileBrowserController {
  void Function(String path, String? selectName)? _handler;
  ({String path, String? selectName})? _pending;

  void open(String path, {String? selectName}) {
    final handler = _handler;
    if (handler == null) {
      _pending = (path: path, selectName: selectName);
    } else {
      handler(path, selectName);
    }
  }

  /// Lo llama la página al montarse; devuelve la petición pendiente, si
  /// la había, para que la aplique en su primera carga.
  ({String path, String? selectName})? attach(
    void Function(String path, String? selectName) handler,
  ) {
    _handler = handler;
    final pending = _pending;
    _pending = null;
    return pending;
  }

  void detach() => _handler = null;
}
