/// Nombres que Windows reserva para dispositivos, con o sin extensión
/// (`CON`, `nul.txt`...): crear un archivo así falla o escribe en el
/// dispositivo, no en disco.
const _reservedWindowsNames = {
  'CON', 'PRN', 'AUX', 'NUL', //
  'COM1', 'COM2', 'COM3', 'COM4', 'COM5', 'COM6', 'COM7', 'COM8', 'COM9',
  'LPT1', 'LPT2', 'LPT3', 'LPT4', 'LPT5', 'LPT6', 'LPT7', 'LPT8', 'LPT9',
};

/// Convierte un nombre del servidor en uno seguro para crear en el disco
/// local cuando NO pasa por el diálogo de guardar del sistema (descarga de
/// varios archivos a una carpeta). El servidor ya rechaza separadores y
/// caracteres de control, pero no los nombres reservados de Windows ni los
/// puntos o espacios finales (que Windows elimina en silencio). Se aplica
/// en todas las plataformas: el mismo nombre debe funcionar en todas.
String safeLocalFileName(String name) {
  // Defensa en profundidad: nunca una ruta, solo un nombre.
  var safe = name.replaceAll(RegExp(r'[\\/:*?"<>|\x00-\x1F]'), '_');
  safe = safe.replaceAll(RegExp(r'[. ]+$'), '');
  if (safe.isEmpty) return '_';
  final stem = safe.split('.').first.trimRight().toUpperCase();
  if (_reservedWindowsNames.contains(stem)) safe = '_$safe';
  return safe;
}
