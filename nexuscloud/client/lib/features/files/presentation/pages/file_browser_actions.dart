part of 'file_browser_page.dart';

enum _MenuAction {
  open,
  preview,
  download,
  rename,
  move,
  share,
  versions,
  trash,
  newFolder,
  upload,
  refresh,
  selectAll,
}

/// Acciones del explorador (subir, descargar, papelera, renombrar, crear,
/// mover, compartir, versiones) y su menú contextual. Separadas de la
/// página solo por tamaño: comparten su estado a través de los miembros
/// abstractos de abajo, que implementa `_FileBrowserPageState`.
mixin _BrowserActions on State<FileBrowserPage> {
  FilesRepository get _filesRepository;
  TransferQueue get _transfers;
  String get _currentPath;
  DirectoryListing? get _listing;
  List<BrowserItem> get _visible;
  Set<String> get _selected;
  set _selected(Set<String> value);
  set _anchorIndex(int? value);
  set _cursorIndex(int? value);
  List<BrowserItem> get _selectedItems;
  Future<void> _load(String path, {String? selectName});
  Future<void> _reload();
  void _openItem(BrowserItem item);
  void _selectAll();

  /// Vista previa de [item] (§35). Se puede pasar al resto de archivos de
  /// la carpeta, en el orden en que se ven, sin cerrarla.
  void _preview(BrowserItem item) {
    final files = [
      for (final i in _visible)
        if (i.file != null) i.file!,
    ];
    final index = files.indexWhere((f) => f.id == item.id);
    if (index < 0) return;
    showFilePreview(
      context,
      files: files,
      initialIndex: index,
      onDownload: (file) => _download([BrowserItem.file(file)]),
    );
  }

  void _toast(String message, {bool error = false}) {
    final messenger = ScaffoldMessenger.maybeOf(context);
    if (messenger == null) return;
    messenger
      ..hideCurrentSnackBar()
      ..showSnackBar(
        SnackBar(
          content: Row(
            children: [
              Icon(
                error
                    ? Icons.error_outline_rounded
                    : Icons.check_circle_outline_rounded,
                size: 18,
                color: error ? context.palette.danger : context.palette.success,
              ),
              const SizedBox(width: 10),
              Expanded(child: Text(message)),
            ],
          ),
        ),
      );
  }

  Future<void> _uploadPicked() async {
    final picked = await openFiles();
    if (picked.isEmpty || !mounted) return;
    await _uploadLocalPaths([for (final x in picked) x.path]);
  }

  /// Sube archivos y carpetas locales (desde el selector o soltados desde
  /// el Explorador) a la carpeta actual, de uno en uno. Una carpeta se
  /// recrea en el servidor nivel a nivel antes de subir su contenido.
  /// Recarga una única vez al final del lote: el listado siempre viene de
  /// la verdad del servidor, y recargar entre archivo y archivo solo
  /// produciría parpadeo.
  Future<void> _uploadLocalPaths(List<String> paths) async {
    final target = _currentPath;
    for (final path in paths) {
      if (await FileSystemEntity.isDirectory(path)) {
        await _uploadDirectory(Directory(path), target);
      } else {
        await _uploadOne(path, p.basename(path), target);
      }
    }
    if (mounted && _currentPath == target) await _reload();
  }

  Future<void> _uploadDirectory(
    Directory directory,
    String parentRemote,
  ) async {
    final name = p.basename(directory.path);
    try {
      await _filesRepository.createDirectory(
        parentPath: parentRemote,
        name: name,
      );
    } on ApiException catch (e) {
      _toast('No se pudo crear la carpeta «$name»: ${e.message}', error: true);
      return;
    }
    final remote = RemotePath.join(parentRemote, name);
    final List<FileSystemEntity> children;
    try {
      children = await directory.list(followLinks: false).toList();
    } on FileSystemException {
      _toast('No se pudo leer la carpeta local «$name».', error: true);
      return;
    }
    children.sort((a, b) => a.path.compareTo(b.path));
    for (final child in children) {
      if (child is Directory) {
        await _uploadDirectory(child, remote);
      } else if (child is File) {
        await _uploadOne(child.path, p.basename(child.path), remote);
      }
    }
  }

  Future<void> _uploadOne(
    String localPath,
    String name,
    String parentRemote,
  ) async {
    final item = _transfers.start(
      TransferKind.upload,
      name,
      detail: parentRemote,
    );
    try {
      await _filesRepository.uploadFile(
        parentPath: parentRemote,
        localFilePath: localPath,
        fileName: name,
        onProgress: (done, total) => _transfers.progress(item, done, total),
      );
      _transfers.succeed(item);
    } on ApiException catch (e) {
      _transfers.fail(item, e.message);
    } on FileSystemException {
      _transfers.fail(item, 'No se pudo leer el archivo local.');
    }
  }

  Future<void> _download(List<BrowserItem> items) async {
    final files = [
      for (final i in items)
        if (!i.isDirectory) i.file!,
    ];
    if (files.isEmpty) {
      _toast(
        'Las carpetas no se descargan desde aquí: vincúlalas en «Sincronización».',
        error: true,
      );
      return;
    }
    if (files.length == 1) {
      final location = await getSaveLocation(suggestedName: files.first.name);
      if (location == null || !mounted) return;
      await _downloadOne(files.first, location.path);
      return;
    }
    final directory = await getDirectoryPath(
      confirmButtonText: 'Descargar aquí',
    );
    if (directory == null || !mounted) return;
    for (final file in files) {
      await _downloadOne(file, _uniqueLocalPath(directory, file.name));
    }
  }

  /// `informe.pdf` → `informe (1).pdf` si ya existe en la carpeta elegida
  /// (con descarga múltiple no hay diálogo de "¿reemplazar?" por archivo).
  String _uniqueLocalPath(String directory, String remoteName) {
    final name = safeLocalFileName(remoteName);
    var candidate = p.join(directory, name);
    final ext = p.extension(name);
    final base = p.basenameWithoutExtension(name);
    var n = 1;
    while (File(candidate).existsSync()) {
      candidate = p.join(directory, '$base ($n)$ext');
      n++;
    }
    return candidate;
  }

  Future<void> _downloadOne(FileEntry file, String saveTo) async {
    final item = _transfers.start(
      TransferKind.download,
      file.name,
      detail: saveTo,
    );
    try {
      await _filesRepository.downloadFile(
        file: file,
        saveToPath: saveTo,
        onProgress: (done, total) => _transfers.progress(item, done, total),
      );
      _transfers.succeed(item);
    } on ApiException catch (e) {
      _transfers.fail(item, e.message);
    } on FileSystemException {
      _transfers.fail(item, 'No se pudo escribir el archivo en disco.');
    }
  }

  Future<void> _trash(List<BrowserItem> items) async {
    if (items.isEmpty) return;
    final single = items.length == 1 ? items.first : null;
    final confirmed = await showConfirmDialog(
      context,
      title: single == null
          ? 'Mover ${items.length} elementos a la papelera'
          : single.isDirectory
          ? 'Mover carpeta a la papelera'
          : 'Mover a la papelera',
      message: single == null
          ? 'Los elementos seleccionados se moverán a la papelera. Podrás '
                'restaurarlos desde ahí mientras no se purguen automáticamente.'
          : '"${single.name}" se moverá a la papelera. Podrás restaurarlo '
                'desde ahí mientras no se purgue automáticamente.',
      confirmLabel: 'Mover a la papelera',
      danger: true,
    );
    if (!confirmed || !mounted) return;

    final failures = <String>[];
    for (final item in items) {
      try {
        if (item.isDirectory) {
          await _filesRepository.deleteDirectory(item.id);
        } else {
          await _filesRepository.deleteFile(item.id);
        }
      } on ApiException catch (e) {
        // Mismo criterio que el cliente web: "not_empty" se sustituye por
        // un mensaje más claro en vez de mostrar el genérico del servidor.
        failures.add(
          e.code == 'not_empty'
              ? 'Esa carpeta no está vacía: elimina primero su contenido.'
              : e.message,
        );
        // Sesión caducada: el resto del lote fallaría igual (y la app ya
        // vuelve al login).
        if (e.statusCode == 401) break;
      }
    }
    if (!mounted) return;
    await _reload();
    if (failures.isEmpty) {
      _toast(
        single != null
            ? '«${single.name}» se movió a la papelera.'
            : '${items.length} elementos movidos a la papelera.',
      );
    } else if (items.length == 1) {
      _toast(failures.first, error: true);
    } else {
      _toast(
        'No se pudieron mover ${failures.length} de ${items.length} '
        'elementos: ${failures.first}',
        error: true,
      );
    }
  }

  Future<void> _rename(BrowserItem item) async {
    final name = item.name;
    final dot = name.lastIndexOf('.');
    final baseEnd = (!item.isDirectory && dot > 0) ? dot : name.length;
    String? renamedTo;
    final ok = await showTextInputDialog(
      context,
      title: item.isDirectory ? 'Renombrar carpeta' : 'Renombrar archivo',
      label: 'Nuevo nombre',
      confirmLabel: 'Renombrar',
      icon: Icons.drive_file_rename_outline_rounded,
      initialValue: name,
      initialSelection: TextSelection(baseOffset: 0, extentOffset: baseEnd),
      onSubmit: (value) async {
        if (value == name) return null;
        try {
          if (item.isDirectory) {
            await _filesRepository.moveDirectory(item.id, newName: value);
          } else {
            await _filesRepository.moveFile(item.id, newName: value);
          }
          renamedTo = value;
          return null;
        } on ApiException catch (e) {
          return e.message;
        }
      },
    );
    if (ok && renamedTo != null && mounted) {
      await _load(_currentPath, selectName: renamedTo);
    }
  }

  Future<void> _createFolder() async {
    final existing = {
      for (final d in _listing?.directories ?? const []) d.name.toLowerCase(),
      for (final f in _listing?.files ?? const <FileEntry>[])
        f.name.toLowerCase(),
    };
    String? created;
    final ok = await showTextInputDialog(
      context,
      title: 'Nueva carpeta',
      label: 'Nombre de la carpeta',
      confirmLabel: 'Crear',
      icon: Icons.create_new_folder_outlined,
      initialValue: 'Nueva carpeta',
      validator: (value) => existing.contains(value.toLowerCase())
          ? 'Ya hay un elemento con ese nombre aquí.'
          : null,
      onSubmit: (value) async {
        try {
          await _filesRepository.createDirectory(
            parentPath: _currentPath,
            name: value,
          );
          created = value;
          return null;
        } on ApiException catch (e) {
          return e.message;
        }
      },
    );
    if (ok && created != null && mounted) {
      await _load(_currentPath, selectName: created);
    }
  }

  Future<void> _move(List<BrowserItem> items) async {
    if (items.isEmpty) return;
    final excluded = {
      for (final i in items)
        if (i.isDirectory) RemotePath.join(_currentPath, i.name),
    };
    final destination = await showFolderPickerDialog(
      context,
      filesRepository: _filesRepository,
      initialPath: _currentPath,
      title: items.length == 1
          ? 'Mover «${items.first.name}»'
          : 'Mover ${items.length} elementos',
      excludedPaths: excluded,
    );
    if (destination == null || destination == _currentPath || !mounted) return;

    final failures = <String>[];
    for (final item in items) {
      try {
        if (item.isDirectory) {
          await _filesRepository.moveDirectory(
            item.id,
            newParentPath: destination,
          );
        } else {
          await _filesRepository.moveFile(item.id, newParentPath: destination);
        }
      } on ApiException catch (e) {
        failures.add('«${item.name}»: ${e.message}');
        if (e.statusCode == 401) break;
      }
    }
    if (!mounted) return;
    await _reload();
    final label = destination == RemotePath.root
        ? 'Mis archivos'
        : p.posix.basename(destination);
    if (failures.isEmpty) {
      _toast(
        items.length == 1
            ? 'Movido a «$label».'
            : '${items.length} elementos movidos a «$label».',
      );
    } else {
      _toast(
        'No se pudieron mover ${failures.length} elementos. ${failures.first}',
        error: true,
      );
    }
  }

  Future<void> _share(BrowserItem item) async {
    await showPanelDialog<void>(
      context,
      width: 620,
      height: 720,
      child: SharePage(
        resourceId: item.id,
        resourceName: item.name,
        resourceType: item.isDirectory
            ? ShareResourceType.directory
            : ShareResourceType.file,
      ),
    );
  }

  Future<void> _showVersions(BrowserItem item) async {
    final file = item.file;
    if (file == null) return;
    await showPanelDialog<void>(
      context,
      width: 620,
      child: FileVersionsPage(file: file, onRestored: _reload),
    );
  }

  // -------------------------------------------------------------------------
  // Menú contextual
  // -------------------------------------------------------------------------

  Future<void> _showContextMenu(int? index, Offset globalPosition) async {
    List<BrowserItem> targets = const [];
    if (index != null && index < _visible.length) {
      final item = _visible[index];
      if (!_selected.contains(item.key)) {
        setState(() {
          _selected = {item.key};
          _anchorIndex = index;
          _cursorIndex = index;
        });
      }
      targets = _selectedItems;
    }

    final overlay = Overlay.of(context).context.findRenderObject() as RenderBox;
    final position = RelativeRect.fromRect(
      globalPosition & const Size(1, 1),
      Offset.zero & overlay.size,
    );

    final entries = targets.isEmpty ? _backgroundMenu() : _itemMenu(targets);
    final action = await showMenu<_MenuAction>(
      context: context,
      position: position,
      items: entries,
      constraints: const BoxConstraints(minWidth: 230),
    );
    if (action == null || !mounted) return;
    _runMenuAction(action, targets);
  }

  List<PopupMenuEntry<_MenuAction>> _itemMenu(List<BrowserItem> targets) {
    final single = targets.length == 1 ? targets.first : null;
    final hasFiles = targets.any((t) => !t.isDirectory);
    return [
      if (single != null && single.isDirectory)
        _menuItem(
          _MenuAction.open,
          Icons.folder_open_outlined,
          'Abrir',
          shortcut: 'Intro',
        ),
      if (single != null && !single.isDirectory)
        _menuItem(
          _MenuAction.preview,
          Icons.visibility_outlined,
          'Vista previa',
          shortcut: 'Intro',
        ),
      if (hasFiles)
        _menuItem(
          _MenuAction.download,
          Icons.download_rounded,
          targets.length > 1
              ? 'Descargar ${targets.where((t) => !t.isDirectory).length} archivos'
              : 'Descargar',
        ),
      if (single != null)
        _menuItem(
          _MenuAction.share,
          Icons.person_add_alt_outlined,
          'Compartir',
        ),
      const PopupMenuDivider(height: 8),
      if (single != null)
        _menuItem(
          _MenuAction.rename,
          Icons.drive_file_rename_outline_rounded,
          'Renombrar',
          shortcut: 'F2',
        ),
      _menuItem(_MenuAction.move, Icons.drive_file_move_outline, 'Mover a…'),
      if (single != null && !single.isDirectory)
        _menuItem(
          _MenuAction.versions,
          Icons.history_rounded,
          'Historial de versiones',
        ),
      const PopupMenuDivider(height: 8),
      _menuItem(
        _MenuAction.trash,
        Icons.delete_outline_rounded,
        'Mover a la papelera',
        shortcut: 'Supr',
        danger: true,
      ),
    ];
  }

  List<PopupMenuEntry<_MenuAction>> _backgroundMenu() => [
    _menuItem(
      _MenuAction.newFolder,
      Icons.create_new_folder_outlined,
      'Nueva carpeta',
      shortcut: 'Ctrl+N',
    ),
    _menuItem(
      _MenuAction.upload,
      Icons.upload_rounded,
      'Subir archivos',
      shortcut: 'Ctrl+U',
    ),
    const PopupMenuDivider(height: 8),
    _menuItem(
      _MenuAction.selectAll,
      Icons.select_all_rounded,
      'Seleccionar todo',
      shortcut: 'Ctrl+A',
    ),
    _menuItem(
      _MenuAction.refresh,
      Icons.refresh_rounded,
      'Actualizar',
      shortcut: 'F5',
    ),
  ];

  PopupMenuItem<_MenuAction> _menuItem(
    _MenuAction value,
    IconData icon,
    String label, {
    String? shortcut,
    bool danger = false,
  }) => contextMenuItem(value, icon, label, shortcut: shortcut, danger: danger);

  void _runMenuAction(_MenuAction action, List<BrowserItem> targets) {
    switch (action) {
      case _MenuAction.open:
        if (targets.isNotEmpty) _openItem(targets.first);
      case _MenuAction.preview:
        if (targets.length == 1) _preview(targets.first);
      case _MenuAction.download:
        _download(targets);
      case _MenuAction.rename:
        if (targets.length == 1) _rename(targets.first);
      case _MenuAction.move:
        _move(targets);
      case _MenuAction.share:
        if (targets.length == 1) _share(targets.first);
      case _MenuAction.versions:
        if (targets.length == 1) _showVersions(targets.first);
      case _MenuAction.trash:
        _trash(targets);
      case _MenuAction.newFolder:
        _createFolder();
      case _MenuAction.upload:
        _uploadPicked();
      case _MenuAction.refresh:
        _reload();
      case _MenuAction.selectAll:
        _selectAll();
    }
  }
}
