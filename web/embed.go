// Package web embebe los assets estáticos compilados de la interfaz web
// (generados por `npm run build` en este mismo directorio) para que el
// binario Go los sirva sin depender de ficheros externos en producción.
// No vive bajo internal/ porque go:embed no puede referenciar una ruta
// fuera del árbol del paquete que la embebe, y dist/ es la salida natural
// de las herramientas de frontend (Vite) en la raíz de este directorio.
package web

import "embed"

//go:embed all:dist
var DistFS embed.FS
