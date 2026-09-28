import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/network/api_exception.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../../core/theme/app_theme.dart';
import '../../../../core/widgets/confirm_dialog.dart';
import '../../../../core/widgets/file_type_icon.dart';
import '../../../../core/widgets/page_scaffold.dart';
import '../../../../core/widgets/view_states.dart';
import '../../domain/entities/group.dart';
import '../../domain/entities/share.dart';
import '../../domain/repositories/sharing_repository.dart';
import '../share_formatting.dart';
import 'my_shares_page.dart' show ShareRow;

enum _LoadState { loading, loaded, error }

/// Gestión de comparticiones de UN recurso -- calcado de
/// `web/src/components/ShareDialog.tsx` (misma UX, mismo backend), como
/// página completa (con su `Scaffold`) que el explorador abre en un panel
/// sobre la carpeta actual (`showPanelDialog`), igual que
/// `FileVersionsPage`: `showConfirmDialog` es del tamaño de un sí/no, no de
/// un formulario completo.
///
/// A diferencia de "Restaurar" (nunca pide confirmación en este cliente),
/// "Revocar" SÍ la pide -- desviación deliberada de la web, que revoca
/// sin confirmar: no hay endpoint de "des-revocar" (revocar es
/// irreversible sin vuelta atrás posible), y el criterio ya escrito en
/// `confirm_dialog.dart` es justo ese, confirmar lo irreversible con
/// coste real.
class SharePage extends StatefulWidget {
  const SharePage({
    super.key,
    required this.resourceId,
    required this.resourceName,
    required this.resourceType,
  });

  final String resourceId;
  final String resourceName;
  final ShareResourceType resourceType;

  @override
  State<SharePage> createState() => _SharePageState();
}

class _SharePageState extends State<SharePage> {
  final SharingRepository _sharingRepository = sl<SharingRepository>();

  _LoadState _state = _LoadState.loading;
  String? _errorMessage;
  List<Group> _groups = [];
  List<Share> _activeShares = [];

  ShareType _selectedType = ShareType.user;
  final _usernameController = TextEditingController();
  Group? _selectedGroup;
  final _labelController = TextEditingController();
  final _passwordController = TextEditingController();
  final _maxDownloadsController = TextEditingController();
  DateTime? _expiresAt;
  bool _canUpload = false;

  bool _submitting = false;
  String? _createErrorMessage;
  Share? _createdLinkShare;
  String? _createdLinkUrl;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _usernameController.dispose();
    _labelController.dispose();
    _passwordController.dispose();
    _maxDownloadsController.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() => _state = _LoadState.loading);
    try {
      final results = await Future.wait([
        _sharingRepository.listGroups(),
        _sharingRepository.listShares(direction: ShareDirection.byMe),
      ]);
      if (!mounted) return;
      final groups = results[0] as List<Group>;
      final allShares = results[1] as List<Share>;
      setState(() {
        _groups = groups;
        _activeShares = allShares
            .where((s) => s.resourceId == widget.resourceId)
            .toList();
        _state = _LoadState.loaded;
      });
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() {
        _errorMessage = e.message;
        _state = _LoadState.error;
      });
    }
  }

  Future<void> _pickExpiration() async {
    final now = DateTime.now();
    final date = await showDatePicker(
      context: context,
      initialDate: _expiresAt ?? now,
      firstDate: now,
      lastDate: now.add(const Duration(days: 3650)),
    );
    if (date == null || !mounted) return;
    final time = await showTimePicker(
      context: context,
      initialTime: TimeOfDay.fromDateTime(_expiresAt ?? now),
    );
    if (time == null || !mounted) return;
    setState(() {
      _expiresAt =
          DateTime(date.year, date.month, date.day, time.hour, time.minute);
    });
  }

  Future<void> _submitCreate() async {
    if (_selectedType == ShareType.user &&
        _usernameController.text.trim().isEmpty) {
      return;
    }
    if (_selectedType == ShareType.group && _selectedGroup == null) return;

    setState(() {
      _submitting = true;
      _createErrorMessage = null;
    });

    try {
      final created = await _sharingRepository.createShare(
        resourceType: widget.resourceType,
        resourceId: widget.resourceId,
        shareType: _selectedType,
        targetUsername:
            _selectedType == ShareType.user ? _usernameController.text.trim() : null,
        targetGroupId:
            _selectedType == ShareType.group ? _selectedGroup?.id : null,
        label: _selectedType == ShareType.link
            ? _labelController.text.trim()
            : null,
        password: _selectedType == ShareType.link
            ? _passwordController.text
            : null,
        expiresAt: _selectedType == ShareType.link ? _expiresAt : null,
        maxDownloads: _selectedType == ShareType.link
            ? int.tryParse(_maxDownloadsController.text.trim())
            : null,
        canUpload: _selectedType == ShareType.link &&
                widget.resourceType == ShareResourceType.directory
            ? _canUpload
            : false,
      );
      String? linkUrl;
      if (_selectedType == ShareType.link) {
        // Se resuelve una sola vez aquí (no en `build()` vía `FutureBuilder`)
        // -- de lo contrario cada reconstrucción de la página crearía una
        // `Future` nueva y volvería a mostrar un parpadeo de carga.
        final serverUrl = await _sharingRepository.serverBaseUrl;
        linkUrl = serverUrl != null ? '$serverUrl/s/${created.token}' : null;
      }
      if (!mounted) return;
      setState(() {
        _submitting = false;
        if (_selectedType == ShareType.link) {
          _createdLinkShare = created;
          _createdLinkUrl = linkUrl;
        }
      });
      await _load();
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() {
        _submitting = false;
        _createErrorMessage = e.message;
      });
    }
  }

  Future<void> _copyLink(String url) async {
    await Clipboard.setData(ClipboardData(text: url));
  }

  Future<void> _revoke(Share share) async {
    final confirmed = await showConfirmDialog(
      context,
      title: 'Revocar compartición',
      message: 'Se revocará el acceso a "${shareTargetLabel(share)}". '
          'Dejará de poder acceder de inmediato y no podrá deshacerse. '
          '¿Continuar?',
      confirmLabel: 'Revocar',
      danger: true,
    );
    if (!confirmed) return;

    try {
      await _sharingRepository.revokeShare(share.id);
      if (mounted) await _load();
    } on ApiException catch (e) {
      if (!mounted) return;
      setState(() {
        _errorMessage = e.message;
        _state = _LoadState.error;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final canPop = Navigator.of(context).canPop();
    return Scaffold(
      appBar: AppBar(
        automaticallyImplyLeading: false,
        toolbarHeight: 64,
        titleSpacing: 20,
        title: Row(
          children: [
            FileTypeIcon.forName(
              widget.resourceName,
              isDirectory: widget.resourceType == ShareResourceType.directory,
              size: 20,
              boxed: true,
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  const Text('Compartir', style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
                  Text(
                    widget.resourceName,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ],
              ),
            ),
          ],
        ),
        actions: [
          IconButton(
            tooltip: 'Actualizar',
            icon: const Icon(Icons.refresh_rounded),
            onPressed: _load,
          ),
          if (canPop)
            IconButton(
              tooltip: 'Cerrar',
              icon: const Icon(Icons.close_rounded),
              onPressed: () => Navigator.of(context).maybePop(),
            ),
          const SizedBox(width: 8),
        ],
      ),
      body: _buildBody(),
    );
  }

  Widget _buildBody() {
    switch (_state) {
      case _LoadState.loading:
        return const LoadingState();
      case _LoadState.error:
        return ErrorState(
          message: _errorMessage ?? 'No se pudo completar la operación.',
          onRetry: _load,
        );
      case _LoadState.loaded:
        final text = Theme.of(context).textTheme;
        return ListView(
          padding: const EdgeInsets.fromLTRB(20, 20, 20, 24),
          children: [
            SectionCard(
              title: 'Añadir acceso',
              icon: Icons.person_add_alt_outlined,
              description: widget.resourceType == ShareResourceType.directory
                  ? 'Comparte esta carpeta con alguien, con un grupo o mediante un enlace.'
                  : 'Comparte este archivo con alguien, con un grupo o mediante un enlace.',
              child: _buildCreateForm(),
            ),
            const SizedBox(height: 16),
            Card(
              clipBehavior: Clip.antiAlias,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Padding(
                    padding: const EdgeInsets.fromLTRB(20, 16, 20, 8),
                    child: Row(
                      children: [
                        Text('Quién tiene acceso', style: text.titleMedium),
                        const SizedBox(width: 8),
                        StatusPill(label: '${_activeShares.length}'),
                      ],
                    ),
                  ),
                  if (_activeShares.isEmpty)
                    Padding(
                      padding: const EdgeInsets.fromLTRB(20, 4, 20, 20),
                      child: Text(
                        'Nadie tiene acceso todavía.',
                        style: text.bodyMedium?.copyWith(color: context.palette.textMuted),
                      ),
                    )
                  else ...[
                    for (final share in _activeShares) _buildShareTile(share),
                    const SizedBox(height: 8),
                  ],
                ],
              ),
            ),
          ],
        );
    }
  }

  Widget _buildShareTile(Share share) {
    return ShareRow(
      share: share,
      trailing: TextButton(
        onPressed: () => _revoke(share),
        style: TextButton.styleFrom(foregroundColor: context.palette.danger),
        child: const Text('Revocar'),
      ),
    );
  }

  Widget _buildCreateForm() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SegmentedButton<ShareType>(
          showSelectedIcon: false,
          segments: const [
            ButtonSegment(
              value: ShareType.user,
              icon: Icon(Icons.person_outline_rounded, size: 18),
              label: Text('Usuario'),
            ),
            ButtonSegment(
              value: ShareType.group,
              icon: Icon(Icons.groups_outlined, size: 18),
              label: Text('Grupo'),
            ),
            ButtonSegment(
              value: ShareType.link,
              icon: Icon(Icons.link_rounded, size: 18),
              label: Text('Enlace'),
            ),
          ],
          selected: {_selectedType},
          onSelectionChanged: (selection) {
            setState(() {
              _selectedType = selection.first;
              _createErrorMessage = null;
              if (_selectedType != ShareType.link) {
                _createdLinkShare = null;
                _createdLinkUrl = null;
              }
            });
          },
        ),
        const SizedBox(height: 16),
        switch (_selectedType) {
          ShareType.user => TextFormField(
              controller: _usernameController,
              decoration: const InputDecoration(
                labelText: 'Nombre de usuario',
                prefixIcon: Icon(Icons.alternate_email_rounded, size: 18),
              ),
              onFieldSubmitted: (_) => _submitCreate(),
            ),
          ShareType.group => DropdownButtonFormField<Group>(
              initialValue: _selectedGroup,
              decoration: const InputDecoration(
                labelText: 'Selecciona un grupo…',
                prefixIcon: Icon(Icons.groups_outlined, size: 18),
              ),
              items: [
                for (final group in _groups)
                  DropdownMenuItem(value: group, child: Text(group.name)),
              ],
              onChanged: (group) => setState(() => _selectedGroup = group),
            ),
          ShareType.link => _buildLinkForm(),
        },
        if (_createErrorMessage != null) ...[
          const SizedBox(height: 12),
          InlineAlert(message: _createErrorMessage!, tone: AlertTone.danger),
        ],
        const SizedBox(height: 16),
        FilledButton(
          onPressed: _submitting ? null : _submitCreate,
          child: _submitting
              ? const SizedBox(
                  width: 20,
                  height: 20,
                  child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white),
                )
              : Text(_selectedType == ShareType.link ? 'Crear enlace' : 'Compartir'),
        ),
        if (_createdLinkShare != null && _selectedType == ShareType.link)
          _buildLinkCreatedBanner(_createdLinkUrl),
      ],
    );
  }

  Widget _buildLinkForm() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        TextFormField(
          controller: _labelController,
          decoration: const InputDecoration(
            labelText: 'Nombre del enlace (opcional)',
            prefixIcon: Icon(Icons.label_outline_rounded, size: 18),
          ),
        ),
        const SizedBox(height: 12),
        TextFormField(
          controller: _passwordController,
          obscureText: true,
          decoration: const InputDecoration(
            labelText: 'Contraseña (opcional)',
            prefixIcon: Icon(Icons.lock_outline_rounded, size: 18),
          ),
        ),
        const SizedBox(height: 12),
        Row(
          children: [
            Expanded(
              child: OutlinedButton.icon(
                onPressed: _pickExpiration,
                icon: const Icon(Icons.event_outlined, size: 18),
                label: Text(
                  _expiresAt == null
                      ? 'Fecha de expiración (opcional)'
                      : 'Expira: ${formatShareDate(_expiresAt!)}',
                ),
              ),
            ),
            if (_expiresAt != null) ...[
              const SizedBox(width: 8),
              TextButton(
                onPressed: () => setState(() => _expiresAt = null),
                child: const Text('Quitar fecha'),
              ),
            ],
          ],
        ),
        const SizedBox(height: 12),
        TextFormField(
          controller: _maxDownloadsController,
          keyboardType: TextInputType.number,
          decoration: const InputDecoration(
            labelText: 'Límite de descargas (opcional)',
            prefixIcon: Icon(Icons.download_outlined, size: 18),
          ),
        ),
        if (widget.resourceType == ShareResourceType.directory) ...[
          const SizedBox(height: 4),
          CheckboxListTile(
            contentPadding: EdgeInsets.zero,
            controlAffinity: ListTileControlAffinity.leading,
            title: const Text('Permitir subir archivos a esta carpeta'),
            value: _canUpload,
            onChanged: (value) => setState(() => _canUpload = value ?? false),
          ),
        ],
      ],
    );
  }

  Widget _buildLinkCreatedBanner(String? url) {
    final p = context.palette;
    return Padding(
      padding: const EdgeInsets.only(top: 16),
      child: Container(
        padding: const EdgeInsets.all(14),
        decoration: BoxDecoration(
          color: p.successSoft,
          border: Border.all(color: p.success.withValues(alpha: 0.4)),
          borderRadius: BorderRadius.circular(10),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.check_circle_rounded, size: 18, color: p.success),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    'Enlace creado — guárdalo ahora, no se volverá a mostrar:',
                    style: TextStyle(color: p.success, fontWeight: FontWeight.w600),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 10),
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
              decoration: BoxDecoration(
                color: p.surface,
                borderRadius: BorderRadius.circular(8),
                border: Border.all(color: p.border),
              ),
              child: Row(
                children: [
                  Expanded(
                    child: SelectableText(
                      url ?? '',
                      style: monoTextStyle.copyWith(fontSize: 13),
                    ),
                  ),
                  const SizedBox(width: 8),
                  FilledButton.icon(
                    onPressed: url == null
                        ? null
                        : () async {
                            await _copyLink(url);
                            if (!mounted) return;
                            ScaffoldMessenger.maybeOf(context)?.showSnackBar(
                              const SnackBar(content: Text('Enlace copiado al portapapeles.')),
                            );
                          },
                    icon: const Icon(Icons.copy_rounded, size: 16),
                    label: const Text('Copiar'),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}
