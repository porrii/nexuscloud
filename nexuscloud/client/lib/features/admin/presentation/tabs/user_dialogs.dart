part of 'users_tab.dart';

// Diálogos de la pestaña Usuarios: alta, edición, añadir a un grupo y
// borrado. Separados de users_tab.dart solo por tamaño.

/// Contraseña aleatoria legible (sin caracteres confusos como 0/O o 1/l).
String _generatePassword() {
  const alphabet = 'abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789';
  final random = Random.secure();
  return List.generate(
    16,
    (_) => alphabet[random.nextInt(alphabet.length)],
  ).join();
}

class _CreateUserDialog extends StatefulWidget {
  const _CreateUserDialog({required this.service});

  final AdminService service;

  @override
  State<_CreateUserDialog> createState() => _CreateUserDialogState();
}

class _CreateUserDialogState extends State<_CreateUserDialog> {
  final _username = TextEditingController();
  final _displayName = TextEditingController();
  final _email = TextEditingController();
  final _password = TextEditingController();
  UserRole _role = UserRole.user;
  QuotaBytes _quota;
  bool _showPassword = false;
  bool _saving = false;
  String? _error;

  @override
  void dispose() {
    _username.dispose();
    _displayName.dispose();
    _email.dispose();
    _password.dispose();
    super.dispose();
  }

  bool get _valid =>
      _username.text.trim().isNotEmpty &&
      _password.text.length >= 8 &&
      _quota != -1;

  Future<void> _submit() async {
    if (!_valid || _saving) return;
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final user = await widget.service.createUser(
        username: _username.text.trim(),
        password: _password.text,
        role: _role,
        displayName: _displayName.text.trim(),
        email: _email.text.trim(),
        quotaBytes: _quota,
        setQuota: _quota != null,
      );
      if (mounted) Navigator.of(context).pop(user);
    } on ApiException catch (e) {
      if (mounted) {
        setState(() {
          _saving = false;
          _error = e.message;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('Nuevo usuario'),
      content: SizedBox(
        width: 460,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              TextField(
                controller: _username,
                autofocus: true,
                decoration: const InputDecoration(
                  labelText: 'Nombre de usuario',
                ),
                onChanged: (_) => setState(() {}),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: _displayName,
                decoration: const InputDecoration(
                  labelText: 'Nombre visible (opcional)',
                ),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: _email,
                decoration: const InputDecoration(
                  labelText: 'Correo (opcional)',
                ),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: _password,
                obscureText: !_showPassword,
                onChanged: (_) => setState(() {}),
                decoration: InputDecoration(
                  labelText: 'Contraseña (mínimo 8 caracteres)',
                  suffixIcon: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      IconButton(
                        tooltip: _showPassword ? 'Ocultar' : 'Mostrar',
                        icon: Icon(
                          _showPassword
                              ? Icons.visibility_off_outlined
                              : Icons.visibility_outlined,
                          size: 18,
                        ),
                        onPressed: () =>
                            setState(() => _showPassword = !_showPassword),
                      ),
                      IconButton(
                        tooltip: 'Generar una contraseña',
                        icon: const Icon(Icons.auto_awesome_outlined, size: 18),
                        onPressed: () => setState(() {
                          _password.text = _generatePassword();
                          _showPassword = true;
                        }),
                      ),
                      IconButton(
                        tooltip: 'Copiar',
                        icon: const Icon(Icons.copy_rounded, size: 18),
                        onPressed: _password.text.isEmpty
                            ? null
                            : () => copySecretToClipboard(
                                context,
                                _password.text,
                                what: 'Contraseña',
                              ),
                      ),
                    ],
                  ),
                ),
              ),
              const SizedBox(height: 12),
              DropdownButtonFormField<UserRole>(
                initialValue: _role,
                decoration: const InputDecoration(labelText: 'Rol'),
                items: [
                  for (final role in UserRole.assignable)
                    DropdownMenuItem(value: role, child: Text(role.label)),
                ],
                onChanged: (role) => setState(() => _role = role ?? _role),
              ),
              const SizedBox(height: 16),
              Text('Cuota', style: Theme.of(context).textTheme.labelLarge),
              const SizedBox(height: 8),
              QuotaEditor(
                initial: null,
                inheritLabel: 'Heredar',
                onChanged: (quota) => setState(() => _quota = quota),
              ),
              if (_error != null) ...[
                const SizedBox(height: 14),
                InlineAlert(message: _error!),
              ],
            ],
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: _saving ? null : () => Navigator.of(context).pop(),
          child: const Text('Cancelar'),
        ),
        FilledButton(
          onPressed: _valid && !_saving ? _submit : null,
          child: Text(_saving ? 'Creando…' : 'Crear usuario'),
        ),
      ],
    );
  }
}

class _EditUserDialog extends StatefulWidget {
  const _EditUserDialog({required this.service, required this.user});

  final AdminService service;
  final AdminUser user;

  @override
  State<_EditUserDialog> createState() => _EditUserDialogState();
}

class _EditUserDialogState extends State<_EditUserDialog> {
  late final _displayName = TextEditingController(
    text: widget.user.displayName,
  );
  late final _email = TextEditingController(text: widget.user.email ?? '');
  late QuotaBytes _quota = widget.user.quotaBytes;
  bool _saving = false;
  String? _error;

  @override
  void dispose() {
    _displayName.dispose();
    _email.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (_quota == -1 || _saving) return;
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final user = await widget.service.updateUser(
        widget.user.id,
        displayName: _displayName.text.trim(),
        email: _email.text.trim(),
        quotaBytes: _quota,
        setQuota: _quota != widget.user.quotaBytes,
      );
      if (mounted) Navigator.of(context).pop(user);
    } on ApiException catch (e) {
      if (mounted) {
        setState(() {
          _saving = false;
          _error = e.message;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final text = Theme.of(context).textTheme;
    return AlertDialog(
      title: Text('Editar a ${widget.user.label}'),
      content: SizedBox(
        width: 460,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text(
                '@${widget.user.username} · creado el '
                '${formatDateTime(widget.user.createdAt)}',
                style: text.bodySmall,
              ),
              const SizedBox(height: 16),
              TextField(
                controller: _displayName,
                autofocus: true,
                decoration: const InputDecoration(labelText: 'Nombre visible'),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: _email,
                decoration: const InputDecoration(labelText: 'Correo'),
              ),
              const SizedBox(height: 16),
              Text('Cuota', style: text.labelLarge),
              const SizedBox(height: 8),
              QuotaEditor(
                initial: widget.user.quotaBytes,
                inheritLabel: 'Heredar',
                onChanged: (quota) => setState(() => _quota = quota),
              ),
              const SizedBox(height: 12),
              Text(
                'El rol y la contraseña de otra cuenta todavía no se pueden '
                'cambiar desde aquí (solo con la CLI del servidor).',
                style: text.bodySmall,
              ),
              if (_error != null) ...[
                const SizedBox(height: 14),
                InlineAlert(message: _error!),
              ],
            ],
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: _saving ? null : () => Navigator.of(context).pop(),
          child: const Text('Cancelar'),
        ),
        FilledButton(
          onPressed: _quota != -1 && !_saving ? _submit : null,
          child: Text(_saving ? 'Guardando…' : 'Guardar'),
        ),
      ],
    );
  }
}

class _AddToGroupDialog extends StatefulWidget {
  const _AddToGroupDialog({required this.service, required this.user});

  final AdminService service;
  final AdminUser user;

  @override
  State<_AddToGroupDialog> createState() => _AddToGroupDialogState();
}

class _AddToGroupDialogState extends State<_AddToGroupDialog> {
  List<AdminGroup>? _groups;
  AdminGroup? _selected;
  bool _saving = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    widget.service
        .listGroups()
        .then((groups) {
          if (mounted) setState(() => _groups = groups);
        })
        .catchError((Object e) {
          if (mounted) {
            setState(() {
              _groups = const [];
              _error = e is ApiException
                  ? e.message
                  : 'No se pudieron cargar los grupos.';
            });
          }
        });
  }

  Future<void> _submit() async {
    final group = _selected;
    if (group == null || _saving) return;
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      await widget.service.addGroupMember(group.id, widget.user.id);
      if (mounted) Navigator.of(context).pop(group.name);
    } on ApiException catch (e) {
      if (mounted) {
        setState(() {
          _saving = false;
          _error = e.message;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final groups = _groups;
    return AlertDialog(
      title: Text('Añadir a ${widget.user.label} a un grupo'),
      content: SizedBox(
        width: 420,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            if (groups == null)
              const Padding(
                padding: EdgeInsets.all(16),
                child: Center(child: CircularProgressIndicator()),
              )
            else if (groups.isEmpty && _error == null)
              const Text(
                'Todavía no hay grupos. Créalos en la pestaña «Grupos».',
              )
            else if (groups.isNotEmpty)
              DropdownButtonFormField<AdminGroup>(
                initialValue: _selected,
                decoration: const InputDecoration(labelText: 'Grupo'),
                items: [
                  for (final g in groups)
                    DropdownMenuItem(value: g, child: Text(g.name)),
                ],
                onChanged: (g) => setState(() => _selected = g),
              ),
            if (_error != null) ...[
              const SizedBox(height: 14),
              InlineAlert(message: _error!),
            ],
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: _saving ? null : () => Navigator.of(context).pop(),
          child: const Text('Cancelar'),
        ),
        FilledButton(
          onPressed: _selected != null && !_saving ? _submit : null,
          child: const Text('Añadir'),
        ),
      ],
    );
  }
}

/// Borrar una cuenta arrastra en cascada sus sesiones, comparticiones y los
/// metadatos de todos sus archivos (ver `docs/administracion.md`): se pide
/// escribir el nombre de usuario para que no ocurra por un clic equivocado.
class _DeleteUserDialog extends StatefulWidget {
  const _DeleteUserDialog({required this.user});

  final AdminUser user;

  @override
  State<_DeleteUserDialog> createState() => _DeleteUserDialogState();
}

class _DeleteUserDialogState extends State<_DeleteUserDialog> {
  final _confirm = TextEditingController();

  @override
  void dispose() {
    _confirm.dispose();
    super.dispose();
  }

  bool get _matches => _confirm.text.trim() == widget.user.username;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return AlertDialog(
      title: Text('Eliminar a ${widget.user.label}'),
      content: SizedBox(
        width: 440,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const InlineAlert(
              message:
                  'Se eliminarán la cuenta, sus sesiones y comparticiones, y '
                  'todos sus archivos y carpetas dejarán de existir para '
                  'NexusCloud (su contenido queda sin referencia en el disco '
                  'del servidor). No se puede deshacer. Si solo quieres '
                  'impedir el acceso, desactívala.',
            ),
            const SizedBox(height: 16),
            Text.rich(
              TextSpan(
                children: [
                  const TextSpan(text: 'Escribe '),
                  TextSpan(
                    text: widget.user.username,
                    style: const TextStyle(fontWeight: FontWeight.w700),
                  ),
                  const TextSpan(text: ' para confirmar:'),
                ],
              ),
              style: TextStyle(color: p.textSecondary),
            ),
            const SizedBox(height: 8),
            TextField(
              controller: _confirm,
              autofocus: true,
              onChanged: (_) => setState(() {}),
            ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(false),
          child: const Text('Cancelar'),
        ),
        FilledButton(
          style: FilledButton.styleFrom(backgroundColor: p.danger),
          onPressed: _matches ? () => Navigator.of(context).pop(true) : null,
          child: const Text('Eliminar para siempre'),
        ),
      ],
    );
  }
}
