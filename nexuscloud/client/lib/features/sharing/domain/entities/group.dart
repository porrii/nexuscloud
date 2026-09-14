import 'package:equatable/equatable.dart';

/// `Equatable` es obligatorio aquí, no cosmético: el desplegable de grupos
/// (`DropdownButtonFormField`) exige que su valor sea `==` a una entrada de
/// `items`. Sin esto, una recarga de la lista de grupos (p.ej. tras
/// reintentar un error) crearía instancias nuevas no-idénticas a la ya
/// seleccionada, y Flutter lanzaría la aserción "There should be exactly
/// one item with [DropdownButton]'s value".
class Group extends Equatable {
  const Group({required this.id, required this.name});

  final String id;
  final String name;

  @override
  List<Object?> get props => [id, name];
}
