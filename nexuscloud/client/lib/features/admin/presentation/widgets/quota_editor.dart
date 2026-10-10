import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../../../core/format/formatters.dart';
import '../../data/admin_models.dart';

const int _gib = 1024 * 1024 * 1024;

/// Texto de una cuota para listas: [inheritLabel] es lo que significa
/// `null` (un usuario hereda; un grupo no aporta cuota).
String describeQuota(QuotaBytes quota, {required String inheritLabel}) {
  if (quota == null) return inheritLabel;
  if (quota == 0) return 'Ilimitada';
  return formatBytes(quota);
}

enum _QuotaMode { inherit, unlimited, limited }

/// Selector de cuota tri-estado (§24, ADR-036). Llama a [onChanged] con el
/// valor listo para enviar en `quota_bytes`, o con `-1` mientras el límite
/// escrito no es válido (el formulario lo usa para desactivar Guardar).
class QuotaEditor extends StatefulWidget {
  const QuotaEditor({
    super.key,
    required this.initial,
    required this.onChanged,
    required this.inheritLabel,
  });

  final QuotaBytes initial;
  final ValueChanged<QuotaBytes> onChanged;

  /// «Heredar» para un usuario, «Sin cuota de grupo» para un grupo.
  final String inheritLabel;

  @override
  State<QuotaEditor> createState() => _QuotaEditorState();
}

class _QuotaEditorState extends State<QuotaEditor> {
  late _QuotaMode _mode = switch (widget.initial) {
    null => _QuotaMode.inherit,
    0 => _QuotaMode.unlimited,
    _ => _QuotaMode.limited,
  };
  late final _gbController = TextEditingController(
    text: (widget.initial ?? 0) > 0 ? _gbText(widget.initial!) : '',
  );

  static String _gbText(int bytes) {
    final gb = bytes / _gib;
    return gb == gb.roundToDouble()
        ? gb.toStringAsFixed(0)
        : gb.toStringAsFixed(2);
  }

  @override
  void dispose() {
    _gbController.dispose();
    super.dispose();
  }

  void _emit() {
    switch (_mode) {
      case _QuotaMode.inherit:
        widget.onChanged(null);
      case _QuotaMode.unlimited:
        widget.onChanged(0);
      case _QuotaMode.limited:
        // Sin editar el número, se devuelve el límite exacto de partida: el
        // texto va redondeado a 2 decimales y reenviarlo cambiaría la cuota
        // (p. ej. 1 234 567 890 B → «1.15» GB → 1 234 803 097 B).
        final initial = widget.initial;
        if (initial != null &&
            initial > 0 &&
            _gbController.text == _gbText(initial)) {
          widget.onChanged(initial);
          return;
        }
        final gb = double.tryParse(_gbController.text.replaceAll(',', '.'));
        widget.onChanged(gb == null || gb <= 0 ? -1 : (gb * _gib).round());
    }
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        SegmentedButton<_QuotaMode>(
          showSelectedIcon: false,
          segments: [
            ButtonSegment(
              value: _QuotaMode.inherit,
              label: Text(widget.inheritLabel),
            ),
            const ButtonSegment(
              value: _QuotaMode.unlimited,
              label: Text('Ilimitada'),
            ),
            const ButtonSegment(
              value: _QuotaMode.limited,
              label: Text('Límite'),
            ),
          ],
          selected: {_mode},
          onSelectionChanged: (value) {
            setState(() => _mode = value.first);
            _emit();
          },
        ),
        if (_mode == _QuotaMode.limited) ...[
          const SizedBox(height: 12),
          SizedBox(
            width: 200,
            child: TextField(
              controller: _gbController,
              autofocus: true,
              keyboardType: const TextInputType.numberWithOptions(
                decimal: true,
              ),
              inputFormatters: [
                FilteringTextInputFormatter.allow(RegExp(r'[0-9.,]')),
              ],
              decoration: const InputDecoration(
                labelText: 'Límite',
                suffixText: 'GB',
              ),
              onChanged: (_) => _emit(),
            ),
          ),
        ],
      ],
    );
  }
}
