import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

/// Atajos que deben funcionar estén donde estén el foco y la sección
/// visible (Ctrl+1…5, Ctrl+K).
///
/// Un `CallbackShortcuts` solo ve las teclas que suben desde el nodo
/// enfocado. Al cambiar de sección, el `ExcludeFocus` del shell le quita el
/// foco a la sección que se oculta y este acaba en el `FocusScope` de la
/// ruta, que está POR ENCIMA del shell: a partir de ahí ningún atajo del
/// shell se disparaba (solo funcionaba el primer Ctrl+número desde
/// el explorador). Por eso se escucha en [HardwareKeyboard], que recibe todas
/// las pulsaciones sin depender del foco.
///
/// No actúa si hay un diálogo o un menú encima de la ruta del shell.
class GlobalShortcuts extends StatefulWidget {
  const GlobalShortcuts({
    super.key,
    required this.bindings,
    required this.child,
  });

  final Map<ShortcutActivator, VoidCallback> bindings;
  final Widget child;

  @override
  State<GlobalShortcuts> createState() => _GlobalShortcutsState();
}

class _GlobalShortcutsState extends State<GlobalShortcuts> {
  @override
  void initState() {
    super.initState();
    HardwareKeyboard.instance.addHandler(_handleKey);
  }

  @override
  void dispose() {
    HardwareKeyboard.instance.removeHandler(_handleKey);
    super.dispose();
  }

  bool _handleKey(KeyEvent event) {
    if (!mounted) return false;
    final route = ModalRoute.of(context);
    if (route != null && !route.isCurrent) return false;
    for (final entry in widget.bindings.entries) {
      if (entry.key.accepts(event, HardwareKeyboard.instance)) {
        entry.value();
        return true;
      }
    }
    return false;
  }

  @override
  Widget build(BuildContext context) => widget.child;
}
