import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/core/paths/local_file_name.dart';

void main() {
  test('deja tal cual un nombre normal', () {
    expect(safeLocalFileName('informe final.pdf'), 'informe final.pdf');
    expect(safeLocalFileName('contrato.v2.docx'), 'contrato.v2.docx');
  });

  test('evita los nombres reservados de Windows, con o sin extensión', () {
    expect(safeLocalFileName('CON'), '_CON');
    expect(safeLocalFileName('nul.txt'), '_nul.txt');
    expect(safeLocalFileName('com1.tar.gz'), '_com1.tar.gz');
    expect(safeLocalFileName('console.log'), 'console.log');
  });

  test('quita puntos y espacios finales y nunca devuelve una ruta', () {
    expect(safeLocalFileName('notas. . '), 'notas');
    expect(safeLocalFileName('...'), '_');
    expect(safeLocalFileName(r'..\..\evil.exe'), '.._.._evil.exe');
    expect(safeLocalFileName('a/b:c'), 'a_b_c');
  });
}
