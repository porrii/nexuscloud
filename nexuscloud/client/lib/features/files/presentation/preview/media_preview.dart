import 'dart:async';

import 'package:flutter/material.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';

import '../../../../core/network/api_exception.dart';
import '../../../../core/network/authorized_stream_proxy.dart';
import '../../../../core/theme/app_palette.dart';
import '../../../../core/widgets/view_states.dart';
import '../../data/file_content_service.dart';
import '../../domain/entities/file_entry.dart';

/// `true` si los primeros bytes son solo texto. Todos los contenedores de
/// vídeo y audio (MP4, MKV/WebM, MPEG-TS, OGG, RIFF, FLAC, MP3...) llevan
/// bytes binarios en su primer kilobyte; uno que sea texto es una lista de
/// reproducción (HLS, DASH, PLS, M3U...) disfrazada de vídeo, que haría al
/// reproductor pedir URLs elegidas por quien subió el archivo.
bool looksLikeTextNotMedia(List<int> prefix) {
  if (prefix.isEmpty) return false;
  for (final byte in prefix) {
    final isControl =
        byte < 0x20 &&
        byte != 0x09 &&
        byte != 0x0A &&
        byte != 0x0C &&
        byte != 0x0D;
    if (isControl || byte == 0x7F) return false;
  }
  return true;
}

/// Endurecimiento de libmpv (ADR-046), verificado con la libmpv real contra
/// mp4, mkv, webm, mp3 (se reproducen), avi (se rechaza) y listas HLS/M3U
/// disfrazadas de .mp4. OJO, comprobado: `demuxer=lavf` y
/// `access-references=no` rompen media_kit (abre cada medio a través de una
/// lista temporal local) y no se pueden usar.
const Map<String, String> kHardenedMpvOptions = {
  // FFmpeg solo puede hablar HTTP/TCP (con el proxy de loopback) y solo
  // abre estos demuxers: un formato raro, con más historial de fallos, ni
  // se intenta. `mov` cubre mp4/m4a/3gp; `matroska` cubre webm.
  'demuxer-lavf-o':
      'protocol_whitelist=[http,tcp],'
      'format_whitelist=[mov,mp4,m4a,3gp,3g2,mj2,matroska,webm,mp3,ogg,'
      'flac,wav,aac]',
  'ytdl': 'no',
  'load-unsafe-playlists': 'no',
  'ordered-chapters': 'no',
  'sub-auto': 'no',
  'audio-file-auto': 'no',
  'cover-art-auto': 'no',
  // media_kit activa la caché en disco: contenido privado de otros usuarios
  // no debe quedarse en ficheros temporales.
  'cache-on-disk': 'no',
};

/// Vídeo o audio con media_kit (libmpv). Se reproduce por partes desde el
/// servidor (`Range`, ADR-010): no hace falta descargarlo entero ni tiene
/// límite de tamaño.
///
/// libmpv NUNCA recibe el token: habla con un [AuthorizedStreamProxy] en
/// 127.0.0.1 que lo añade solo hacia este archivo del servidor. Además solo
/// puede usar HTTP sobre TCP (nada de `file:`, `data:`, UDP...) y no
/// guarda en disco lo que reproduce.
class MediaPreview extends StatefulWidget {
  const MediaPreview({
    super.key,
    required this.file,
    required this.audioOnly,
    required this.content,
  });

  final FileEntry file;
  final bool audioOnly;
  final FileContentService content;

  @override
  State<MediaPreview> createState() => _MediaPreviewState();
}

class _MediaPreviewState extends State<MediaPreview> {
  static const _genericError =
      'No se pudo reproducir el archivo. Puede que el formato no sea '
      'compatible; descárgalo para abrirlo con otra aplicación.';

  /// `null` si libmpv no se pudo cargar en este equipo.
  Player? _player;
  VideoController? _controller;
  AuthorizedStreamProxy? _proxy;
  StreamSubscription<String>? _errors;
  String? _error;
  bool _explaining = false;

  @override
  void initState() {
    super.initState();
    // media_kit se carga aquí, la primera vez que se abre un vídeo o audio,
    // y no al arrancar la app: en un Linux sin libmpv, la app entera no
    // arrancaría por una función opcional.
    try {
      MediaKit.ensureInitialized();
      _player = Player(
        configuration: const PlayerConfiguration(
          protocolWhitelist: ['http', 'tcp'],
        ),
      );
      _controller = VideoController(_player!);
    } catch (e) {
      debugPrint('Reproductor no disponible: $e');
      _error =
          'La reproducción no está disponible en este equipo (falta la '
          'biblioteca libmpv). Descarga el archivo para abrirlo con otra '
          'aplicación.';
      return;
    }
    _errors = _player!.stream.error.listen((message) {
      // libmpv informa en inglés y con detalles internos: al usuario, un
      // mensaje genérico (el detalle no le sirve para nada).
      debugPrint('Reproductor: $message');
      _explainFailure();
    });
    _open();
  }

  Future<void> _open() async {
    try {
      final prefix = await widget.content.fetchPrefix(widget.file.id);
      if (!mounted) return;
      if (looksLikeTextNotMedia(prefix)) {
        setState(
          () => _error =
              'Este archivo no es un vídeo ni un audio de verdad (por dentro '
              'es texto), así que no se reproduce por seguridad.',
        );
        return;
      }
      final source = await widget.content.streamSource(widget.file.id);
      if (!mounted) return;
      final proxy = await AuthorizedStreamProxy.start(
        upstream: source.uri,
        headers: source.headers,
      );
      if (!mounted) {
        await proxy.close();
        return;
      }
      _proxy = proxy;
      final player = _player!;
      final platform = player.platform;
      // Fallar cerrado: sin poder aplicar el endurecimiento, no se
      // reproduce. Si setProperty lanza, el catch de abajo muestra el error
      // y tampoco se llega a abrir nada.
      if (platform is! NativePlayer) {
        throw StateError('reproductor sin endurecimiento disponible');
      }
      for (final option in kHardenedMpvOptions.entries) {
        await platform.setProperty(option.key, option.value);
      }
      if (!mounted) return;
      await player.open(Media(proxy.uri.toString()));
    } catch (_) {
      // Sin esto, un fallo antes de empezar a reproducir (sesión no
      // disponible, libmpv sin cargar) se perdía y la vista se quedaba en
      // negro sin explicación.
      if (mounted) setState(() => _error = _genericError);
    }
  }

  /// libmpv hace sus propias peticiones (a través del proxy), así que un
  /// 401 o un 404 no pasan por el interceptor de ApiClient. Se repite una
  /// petición mínima por la vía normal: si la sesión caducó, el interceptor
  /// lo detecta y la app vuelve al login; si el archivo ya no existe, se
  /// dice eso en vez de culpar al formato.
  Future<void> _explainFailure() async {
    if (_explaining || _error != null) return;
    _explaining = true;
    var message = _genericError;
    try {
      await widget.content.fetchPrefix(widget.file.id, length: 1);
    } on ApiException catch (e) {
      message = e.statusCode == 404
          ? 'El archivo ya no existe en el servidor.'
          : e.message;
    } catch (_) {
      // Sin más información: el mensaje genérico.
    }
    if (mounted) setState(() => _error = message);
  }

  @override
  void dispose() {
    _errors?.cancel();
    _player?.dispose();
    _proxy?.close();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    if (_error != null) {
      return EmptyState(
        icon: Icons.error_outline_rounded,
        title: 'No se puede reproducir',
        message: _error,
      );
    }
    final video = Video(
      controller: _controller!,
      controls: MaterialDesktopVideoControls,
      fill: widget.audioOnly ? Colors.transparent : Colors.black,
    );
    if (!widget.audioOnly) return video;
    return ColoredBox(
      color: p.surfaceMuted,
      child: Stack(
        children: [
          Center(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(Icons.music_note_rounded, size: 72, color: p.textMuted),
                const SizedBox(height: 12),
                Text(
                  widget.file.name,
                  style: Theme.of(context).textTheme.titleMedium,
                  textAlign: TextAlign.center,
                ),
              ],
            ),
          ),
          Positioned.fill(child: video),
        ],
      ),
    );
  }
}
