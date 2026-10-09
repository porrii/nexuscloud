import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexuscloud_client/features/files/data/file_content_service.dart';
import 'package:nexuscloud_client/features/files/domain/entities/file_entry.dart';
import 'package:nexuscloud_client/features/files/presentation/preview/file_preview_dialog.dart';
import 'package:nexuscloud_client/features/files/presentation/preview/preview_kind.dart';

class _FakeContent implements FileContentService {
  _FakeContent(this.contents);

  final Map<String, String> contents;
  final List<String> fetched = [];

  @override
  Future<Uint8List> fetchBytes(String fileId, {int? maxBytes}) async {
    fetched.add(fileId);
    return Uint8List.fromList(utf8.encode(contents[fileId] ?? ''));
  }

  @override
  Future<Uint8List> fetchPrefix(String fileId, {int length = 1024}) =>
      throw UnimplementedError();

  @override
  Future<({Uri uri, Map<String, String> headers})> streamSource(String fileId) =>
      throw UnimplementedError();
}

FileEntry _file(String id, String name, String mime, {int size = 20}) => FileEntry(
      id: id,
      parentPath: '/',
      name: name,
      sizeBytes: size,
      sha256: 'sha-$id',
      mimeType: mime,
      createdAt: DateTime.utc(2026),
      updatedAt: DateTime.utc(2026),
    );

void main() {
  group('previewKindOf (mismo criterio que la web)', () {
    test('clasifica por MIME y extensión', () {
      expect(previewKindOf(_file('1', 'a.jpg', 'image/jpeg')), PreviewKind.image);
      expect(previewKindOf(_file('1', 'a.pdf', 'application/pdf')), PreviewKind.pdf);
      expect(previewKindOf(_file('1', 'a.mp4', 'video/mp4')), PreviewKind.video);
      expect(previewKindOf(_file('1', 'a.mp3', 'audio/mpeg')), PreviewKind.audio);
      expect(previewKindOf(_file('1', 'LEEME.md', 'text/markdown')), PreviewKind.markdown);
      expect(previewKindOf(_file('1', 'main.go', 'text/plain')), PreviewKind.code);
      expect(previewKindOf(_file('1', 'logo.svg', 'image/svg+xml')), PreviewKind.code);
      expect(previewKindOf(_file('1', 'notas.txt', 'text/plain')), PreviewKind.text);
      expect(previewKindOf(_file('1', 'a.zip', 'application/zip')), PreviewKind.unsupported);
    });
  });

  Future<void> open(
    WidgetTester tester, {
    required List<FileEntry> files,
    required FileContentService content,
    int initialIndex = 0,
    ValueChanged<FileEntry>? onDownload,
  }) async {
    await tester.pumpWidget(MaterialApp(
      home: Builder(
        builder: (context) => TextButton(
          onPressed: () => showFilePreview(
            context,
            files: files,
            initialIndex: initialIndex,
            onDownload: onDownload ?? (_) {},
            contentService: content,
          ),
          child: const Text('Abrir'),
        ),
      ),
    ));
    await tester.tap(find.text('Abrir'));
    await tester.pumpAndSettle();
  }

  testWidgets('muestra el contenido de un archivo de texto', (tester) async {
    final content = _FakeContent({'t': 'hola desde el servidor'});
    await open(tester, files: [_file('t', 'notas.txt', 'text/plain')], content: content);

    expect(find.text('notas.txt'), findsOneWidget);
    expect(find.text('hola desde el servidor'), findsOneWidget);
  });

  testWidgets('el Markdown se formatea y no carga imágenes', (tester) async {
    final content = _FakeContent({'m': '# Título\n\n![logo](https://ejemplo.com/p.png)'});
    await open(tester, files: [_file('m', 'LEEME.md', 'text/markdown')], content: content);

    expect(find.text('Título'), findsOneWidget);
    expect(find.text('[imagen: logo]'), findsOneWidget);
    expect(find.byType(Image), findsNothing);
  });

  testWidgets('sin vista previa ofrece descargar', (tester) async {
    final downloaded = <String>[];
    final content = _FakeContent({});
    await open(
      tester,
      files: [_file('z', 'copia.zip', 'application/zip')],
      content: content,
      onDownload: (f) => downloaded.add(f.id),
    );

    expect(find.text('No hay vista previa para este tipo de archivo'), findsOneWidget);
    expect(content.fetched, isEmpty);
    await tester.tap(find.widgetWithText(FilledButton, 'Descargar'));
    expect(downloaded, ['z']);
  });

  testWidgets('un texto demasiado grande no se descarga a memoria', (tester) async {
    final content = _FakeContent({});
    await open(
      tester,
      files: [_file('g', 'enorme.log', 'text/plain', size: kMaxTextPreviewBytes + 1)],
      content: content,
    );

    expect(find.text('Demasiado grande para la vista previa'), findsOneWidget);
    expect(content.fetched, isEmpty);
  });

  testWidgets('pasa al siguiente archivo sin cerrar', (tester) async {
    final content = _FakeContent({'a': 'primero', 'b': 'segundo'});
    await open(
      tester,
      files: [_file('a', 'a.txt', 'text/plain'), _file('b', 'b.txt', 'text/plain')],
      content: content,
    );
    expect(find.text('1 de 2'), findsOneWidget);
    expect(find.text('primero'), findsOneWidget);

    await tester.tap(find.byTooltip('Siguiente'));
    await tester.pumpAndSettle();

    expect(find.text('2 de 2'), findsOneWidget);
    expect(find.text('segundo'), findsOneWidget);
  });
}
