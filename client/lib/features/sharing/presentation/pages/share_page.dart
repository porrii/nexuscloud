import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../../../core/di/service_locator.dart';
import '../../../../core/network/api_exception.dart';
import '../../../../core/widgets/confirm_dialog.dart';
import '../../domain/entities/group.dart';
import '../../domain/entities/share.dart';
import '../../domain/repositories/sharing_repository.dart';
import '../share_formatting.dart';

enum _LoadState { loading, loaded, error }

/// Gestión de comparticiones de UN recurso -- calcado de
/// `web/src/components/ShareDialog.tsx` (misma UX, mismo backend), pero
/// como página empujada con `Navigator.push` en vez de un modal: es el
/// único precedente Flutter de este cliente para "una pantalla ligada a
/// un recurso, empujada desde un icono de fila" (`FileVersionsPage`), y
/// `showConfirmDialog` es del tamaño de un sí/no, no de un formulario
/// completo.
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
    return Scaffold(
      appBar: AppBar(
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            const Text('Compartir', style: TextStyle(fontSize: 16)),
            Text(widget.resourceName, style: Theme.of(context).textTheme.bodySmall),
          ],
        ),
        actions: [
          IconButton(
            tooltip: 'Actualizar',
            icon: const Icon(Icons.refresh),
            onPressed: _load,
          ),
        ],
      ),
      body: _buildBody(),
    );
  }

  Widget _buildBody() {
    switch (_state) {
      case _LoadState.loading:
        return const Center(child: CircularProgressIndicator());
      case _LoadState.error:
        return Center(
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(_errorMessage ?? 'No se pudo completar la operación.'),
                const SizedBox(height: 12),
                OutlinedButton(onPressed: _load, child: const Text('Reintentar')),
              ],
            ),
          ),
        );
      case _LoadState.loaded:
        return ListView(
          padding: const EdgeInsets.all(16),
          children: [
            Text('Comparticiones activas',
                style: Theme.of(context).textTheme.titleSmall),
            const SizedBox(height: 8),
            if (_activeShares.isEmpty)
              const Padding(
                padding: EdgeInsets.symmetric(vertical: 8),
                child: Text('Nadie tiene acceso todavía.'),
              )
            else
              for (final share in _activeShares) _buildShareTile(share),
            const SizedBox(height: 24),
            const Divider(),
            const SizedBox(height: 16),
            Text('Compartir', style: Theme.of(context).textTheme.titleSmall),
            const SizedBox(height: 12),
            _buildCreateForm(),
          ],
        );
    }
  }

  Widget _buildShareTile(Share share) {
    return ListTile(
      contentPadding: EdgeInsets.zero,
      title: Text(shareTargetLabel(share)),
      subtitle: Text(shareMetadataLine(share)),
      trailing: TextButton(
        onPressed: () => _revoke(share),
        child: const Text('Revocar'),
      ),
    );
  }

  Widget _buildCreateForm() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SegmentedButton<ShareType>(
          segments: const [
            ButtonSegment(value: ShareType.user, label: Text('Usuario')),
            ButtonSegment(value: ShareType.group, label: Text('Grupo')),
            ButtonSegment(value: ShareType.link, label: Text('Enlace')),
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
              decoration: const InputDecoration(labelText: 'Nombre de usuario'),
            ),
          ShareType.group => DropdownButtonFormField<Group>(
              initialValue: _selectedGroup,
              decoration:
                  const InputDecoration(labelText: 'Selecciona un grupo…'),
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
          Text(
            _createErrorMessage!,
            style: TextStyle(color: Theme.of(context).colorScheme.error),
          ),
        ],
        const SizedBox(height: 16),
        FilledButton(
          onPressed: _submitting ? null : _submitCreate,
          child: _submitting
              ? const SizedBox(
                  width: 20,
                  height: 20,
                  child: CircularProgressIndicator(strokeWidth: 2),
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
          decoration:
              const InputDecoration(labelText: 'Nombre del enlace (opcional)'),
        ),
        const SizedBox(height: 12),
        TextFormField(
          controller: _passwordController,
          obscureText: true,
          decoration:
              const InputDecoration(labelText: 'Contraseña (opcional)'),
        ),
        const SizedBox(height: 12),
        OutlinedButton(
          onPressed: _pickExpiration,
          child: Text(
            _expiresAt == null
                ? 'Fecha de expiración (opcional)'
                : 'Expira: ${formatShareDate(_expiresAt!)}',
          ),
        ),
        if (_expiresAt != null)
          Align(
            alignment: Alignment.centerRight,
            child: TextButton(
              onPressed: () => setState(() => _expiresAt = null),
              child: const Text('Quitar fecha'),
            ),
          ),
        const SizedBox(height: 12),
        TextFormField(
          controller: _maxDownloadsController,
          keyboardType: TextInputType.number,
          decoration:
              const InputDecoration(labelText: 'Límite de descargas (opcional)'),
        ),
        if (widget.resourceType == ShareResourceType.directory) ...[
          const SizedBox(height: 8),
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
    return Padding(
      padding: const EdgeInsets.only(top: 16),
      child: Container(
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: Colors.green.withValues(alpha: 0.1),
          border: Border.all(color: Colors.green),
          borderRadius: BorderRadius.circular(8),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              'Enlace creado — guárdalo ahora, no se volverá a mostrar:',
            ),
            const SizedBox(height: 8),
            SelectableText(
              url ?? '',
              style: const TextStyle(fontFamily: 'monospace'),
            ),
            const SizedBox(height: 8),
            Align(
              alignment: Alignment.centerRight,
              child: TextButton(
                onPressed: url == null ? null : () => _copyLink(url),
                child: const Text('Copiar'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
