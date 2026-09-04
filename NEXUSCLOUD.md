# PROMPT MAESTRO — NEXUSCLOUD

## 0. ROL Y OBJETIVO

Actúa como un equipo completo de ingeniería de software formado por:

- Arquitectos de software senior.
- Ingenieros backend senior.
- Ingenieros frontend y UX/UI senior.
- Ingenieros de sistemas.
- Ingenieros DevOps.
- Ingenieros de redes.
- Ingenieros especializados en almacenamiento.
- Ingenieros especializados en ciberseguridad.
- QA engineers.
- Ingenieros especializados en aplicaciones multiplataforma.
- Auditores de seguridad.

Tu misión es **diseñar y desarrollar desde cero NexusCloud**, una plataforma de almacenamiento en nube privada, autoalojada, modular, multiplataforma, segura, eficiente y preparada para crecer durante muchos años.

NexusCloud será inicialmente utilizado dentro de una red doméstica/local, pero posteriormente podrá exponerse a Internet mediante un dominio propio.

El diseño debe realizarse **desde el primer día pensando en ese futuro escenario**, sin tener que rediseñar posteriormente la arquitectura.

NexusCloud se distribuirá bajo **licencia MIT**, por lo que debe ser un proyecto realmente reutilizable y configurable por terceros.

---

# 1. VISIÓN DE NEXUSCLOUD

NexusCloud no debe ser simplemente un clon de Google Drive, Dropbox o Nextcloud.

Debe ser una plataforma propia de almacenamiento y servicios que pueda funcionar:

- En un PC Windows.
- En Linux.
- En Raspberry Pi.
- En Docker.
- Opcionalmente en NAS compatibles.
- En redes locales.
- En servidores domésticos.
- En servidores remotos.
- En Internet mediante dominio.

Debe poder utilizarse tanto por una persona como por una familia, pequeño grupo, organización o hasta aproximadamente 100 usuarios.

La arquitectura debe evitar depender de servicios cloud externos.

Debe poder funcionar completamente dentro de una red local sin Internet.

---

# 2. PRINCIPIO FUNDAMENTAL

La prioridad del proyecto será:

1. SEGURIDAD.
2. FIABILIDAD.
3. INTEGRIDAD DE LOS DATOS.
4. PRIVACIDAD.
5. Mantenibilidad.
6. Rendimiento.
7. Escalabilidad.
8. Facilidad de configuración.
9. Experiencia de usuario.
10. Funcionalidades.

Nunca sacrifiques seguridad o integridad de datos para conseguir una funcionalidad secundaria.

Si existe conflicto entre comodidad y seguridad, prioriza seguridad, pero permite que el administrador pueda modificar determinadas políticas de forma explícita cuando sea razonable.

---

# 3. FILOSOFÍA "SECURE BY DEFAULT"

NexusCloud debe seguir estrictamente una filosofía:

> Secure by default.

Una instalación recién realizada debe ser segura sin necesidad de realizar una configuración avanzada.

Todo aquello que pueda aumentar la superficie de ataque debe estar:

- Desactivado por defecto.
- Claramente identificado.
- Configurable.
- Protegido mediante autenticación.
- Registrado en auditoría.

Ejemplos:

- Web pública: desactivada inicialmente si no es necesaria.
- WebDAV: desactivado inicialmente.
- Registro público: desactivado.
- Comparticiones públicas: desactivadas o restringidas.
- API externa: restringida.
- Métodos inseguros: prohibidos.
- HTTP sin cifrado cuando exista acceso remoto: prohibido.
- Credenciales por defecto: prohibidas.
- Contraseñas almacenadas en texto plano: prohibidas.

---

# 4. ARQUITECTURA GENERAL

Utiliza una arquitectura modular y desacoplada.

NO construyas una aplicación monolítica difícil de mantener.

La arquitectura conceptual debe ser similar a:

NexusCloud
│
├── NexusCloud Core
│
├── Authentication
├── Authorization
├── Users
├── Groups
├── Roles
├── Sessions
│
├── Storage Engine
├── Storage Pools
├── Disk Manager
├── Volume Manager
├── RAID Integration
├── File Manager
├── Metadata Manager
│
├── Versioning
├── Trash
├── Snapshots
├── Backup Manager
│
├── Sharing
├── Public Links
├── Invitations
│
├── Sync Engine
├── Upload Manager
├── Download Manager
│
├── Search Engine
├── Thumbnail Engine
├── Preview Engine
│
├── Notification Engine
├── Audit Engine
├── Logging
│
├── API
├── WebDAV
├── Web Interface
│
├── Security Engine
├── Configuration Engine
│
├── Reverse Proxy Integration
├── TLS/HTTPS
│
├── Plugin/Extension System
│
└── Nexus Ecosystem Integration

Cada módulo debe tener responsabilidades claramente separadas.

---

# 5. BACKEND

Utiliza **Go** para el backend.

Motivos:

- Alto rendimiento.
- Excelente concurrencia.
- Bajo consumo de memoria.
- Muy apropiado para servidores domésticos.
- Excelente soporte multiplataforma.
- Excelente compatibilidad con Linux.
- Muy apropiado para Raspberry Pi.
- Facilidad de distribución mediante binarios.
- Excelente soporte para APIs HTTP.
- Buena mantenibilidad.

Utiliza las características modernas y estables disponibles en la versión de Go elegida.

No dependas innecesariamente de frameworks pesados.

Prioriza:

- Código claro.
- Seguridad.
- Testabilidad.
- Mantenibilidad.
- Pocas dependencias.
- Dependencias auditables.

---

# 6. CLIENTES

Los clientes principales serán:

- Windows.
- Linux.
- Android.

La aplicación web también debe existir como cliente opcional.

macOS no es prioritario.

Para los clientes multiplataforma utiliza **Flutter/Dart**, siempre que sea técnicamente razonable.

La arquitectura debe permitir reutilizar la mayor cantidad posible de código entre:

- Windows.
- Linux.
- Android.
- Web.

El cliente debe comunicarse con NexusCloud mediante APIs bien definidas.

---

# 7. ESTRUCTURA DEL PROYECTO

Diseña una estructura profesional.

Por ejemplo:

/nexuscloud

/backend
    /cmd
    /internal
    /pkg
    /api
    /auth
    /storage
    /users
    /sharing
    /sync
    /security
    /backup
    /audit
    /search
    /config

/client
    /lib
    /android
    /windows
    /linux
    /web

/docs

/scripts

/deploy

/docker

/tests

/config

/migrations

No copies exactamente esta estructura si existe una alternativa arquitectónicamente superior.

Explica la decisión en la documentación.

---

# 8. BASE DE DATOS

No acoples NexusCloud obligatoriamente a una única base de datos.

Implementa una capa de abstracción.

Debe existir al menos:

### Base de datos por defecto

SQLite.

Ideal para:

- Instalaciones pequeñas.
- Raspberry.
- PCs domésticos.
- Uso personal.

### Base de datos avanzada

PostgreSQL.

Para instalaciones mayores o con más usuarios.

Siempre que sea razonable, permite también:

- MariaDB/MySQL.

La elección debe realizarse mediante configuración.

No permitas que cambiar de base de datos implique modificar el código de NexusCloud.

Implementa migraciones de base de datos profesionales.

Nunca borres datos automáticamente durante una migración.

---

# 9. SISTEMA DE ALMACENAMIENTO

El sistema de almacenamiento debe ser uno de los módulos más importantes.

No almacenes los archivos completos dentro de la base de datos.

La base de datos debe contener principalmente:

- Metadatos.
- Usuarios.
- Permisos.
- Rutas.
- Versiones.
- Comparticiones.
- Hashes.
- Información de almacenamiento.
- Configuración.

Los archivos deben almacenarse en el sistema de archivos.

---

# 10. STORAGE POOLS

Implementa un concepto de:

Storage Pool.

Un administrador podrá crear pools de almacenamiento.

Ejemplo:

Pool principal
├── Disco 1
├── Disco 2
└── Disco 3

Pool secundario
├── SSD
└── SSD

Backup
└── HDD externo

Cada pool debe permitir configurar:

- Nombre.
- Ubicación.
- Capacidad.
- Estado.
- Tipo.
- Prioridad.
- Política de utilización.
- Política de backup.
- Política de versionado.
- Política de snapshots.

---

# 11. DISCOS

NexusCloud debe detectar los dispositivos de almacenamiento disponibles siempre que el sistema operativo lo permita.

Mostrar:

- Nombre.
- Modelo.
- Fabricante.
- Capacidad.
- Espacio utilizado.
- Espacio libre.
- Tipo.
- Sistema de archivos.
- Estado.
- Punto de montaje.
- Número de serie cuando sea seguro y apropiado.
- Temperatura cuando el sistema lo permita.
- SMART cuando esté disponible.

Nunca dependas de una única API del sistema operativo.

Implementa adaptadores específicos:

Windows
Linux
Raspberry Pi/Linux

---

# 12. RAID

NO implementes un sistema RAID propietario dentro de NexusCloud.

Eso sería peligroso y aumentaría enormemente la complejidad.

NexusCloud debe detectar y utilizar tecnologías de almacenamiento proporcionadas por el sistema operativo o hardware.

Debe ser compatible, cuando el sistema lo permita, con:

- RAID 0.
- RAID 1.
- RAID 5.
- RAID 6.
- RAID 10.
- JBOD.
- RAID hardware.
- Linux mdadm.
- Windows Storage Spaces.

NexusCloud debe mostrar el estado del RAID cuando sea posible.

Debe advertir claramente:

- Disco degradado.
- Disco fallido.
- Rebuild.
- Capacidad reducida.
- Riesgo de pérdida de datos.

Nunca afirmar que un RAID constituye un backup.

---

# 13. SISTEMA DE ARCHIVOS

Prioriza una estructura basada en carpetas.

El usuario debe percibir NexusCloud como un explorador de archivos moderno.

Ejemplo:

NexusCloud
│
├── Documentos
├── Fotos
├── Vídeos
├── Trabajo
├── Proyectos
└── Compartido

No expongas innecesariamente al usuario la complejidad interna del Storage Engine.

---

# 14. INTEGRIDAD DE DATOS

Todos los archivos importantes deben poder disponer de:

- Hash.
- Tamaño.
- Fecha.
- Metadatos.
- Identificador único.

Utiliza hashes criptográficos modernos.

Permite verificar integridad.

El sistema debe detectar archivos corruptos cuando sea posible.

No utilices MD5 como mecanismo principal de integridad de seguridad.

---

# 15. VERSIONADO

Implementa versionado de archivos.

Debe ser:

- Activable/desactivable.
- Configurable por administrador.
- Preferiblemente configurable por usuario cuando las políticas lo permitan.

Configurar:

- Número máximo de versiones.
- Tiempo de conservación.
- Espacio máximo.
- Política automática de limpieza.

Ejemplo:

documento.pdf

v1
v2
v3
v4

Permitir restaurar versiones anteriores.

---

# 16. PAPELERA

Implementa papelera.

Configurable:

- Activada/desactivada.
- Días de conservación.
- Tamaño máximo.
- Limpieza automática.

Nunca elimines permanentemente un archivo sin respetar las políticas configuradas.

---

# 17. SNAPSHOTS

Integra snapshots siempre que el sistema de almacenamiento subyacente lo permita.

No implementes un sistema de snapshots propietario si el sistema operativo o filesystem ya proporciona uno fiable.

Detecta:

- ZFS.
- Btrfs.
- Storage Spaces.
- Otros sistemas compatibles.

La funcionalidad debe ser modular.

Documenta claramente las capacidades disponibles según plataforma.

---

# 18. BACKUPS

Implementa un Backup Manager independiente.

Debe permitir:

- Backup automático.
- Backup manual.
- Backup incremental cuando sea viable.
- Backup completo.
- Programación.
- Retención.
- Destino configurable.
- Verificación.
- Logs.
- Restauración.

Destinos posibles:

- Disco local.
- Disco USB.
- Otra carpeta.
- Otro servidor NexusCloud.
- SMB/NFS cuando sea apropiado.
- Otros destinos mediante plugins.

Nunca presentar RAID como backup.

---

# 19. REGLA 3-2-1

El sistema debe poder documentar y recomendar una estrategia 3-2-1:

3 copias
2 medios diferentes
1 copia fuera del equipo principal

La interfaz debe mostrar advertencias cuando el usuario no tenga ninguna estrategia de backup.

---

# 20. USUARIOS

Sistema completo de usuarios.

Cada usuario tendrá:

- ID.
- Nombre.
- Nombre visible.
- Email opcional.
- Avatar opcional.
- Estado.
- Roles.
- Grupos.
- Cuota.
- Uso de almacenamiento.
- Fecha de creación.
- Último acceso.
- Métodos de autenticación.
- Sesiones activas.

---

# 21. REGISTRO DE USUARIOS

No permitir registro público por defecto.

Los nuevos usuarios se crean mediante:

- Administrador.
- Invitación.

Las invitaciones deben:

- Ser únicas.
- Tener expiración.
- Poder revocarse.
- Tener uso limitado.
- Registrar quién la creó.
- Registrar cuándo fue utilizada.

---

# 22. ROLES

Implementa RBAC.

Roles iniciales:

- Super Admin.
- Administrator.
- User.
- Read Only.

Permite crear roles personalizados.

Los permisos deben ser granulares.

---

# 23. GRUPOS

Implementa grupos.

Ejemplo:

Familia
Trabajo
Clientes
Administradores

Permitir asignar permisos a grupos.

---

# 24. CUOTAS

Cada usuario puede tener una cuota.

Ejemplo:

Ivan
100 GB

Familia
500 GB

Configurable:

- Global.
- Usuario.
- Grupo.

---

# 25. AUTENTICACIÓN

Utiliza estándares modernos y seguros.

Debe soportar como mínimo:

- Usuario + contraseña.
- Sesiones seguras.
- TOTP.

Preparar arquitectura para:

- Passkeys.
- WebAuthn.
- FIDO2.

No almacenar contraseñas.

Utilizar algoritmos modernos de hashing de contraseñas como Argon2id cuando sea apropiado.

---

# 26. SESIONES

Implementar:

- Expiración.
- Revocación.
- Logout remoto.
- Lista de sesiones.
- Dispositivo.
- IP.
- Fecha.
- Última actividad.

El usuario podrá cerrar sesiones remotamente.

El administrador podrá revocar sesiones.

---

# 27. FUERZA BRUTA

Protección contra:

- Brute force.
- Credential stuffing.
- Login flooding.
- Enumeración de usuarios.

Implementa:

- Rate limiting.
- Backoff.
- Bloqueos temporales cuando corresponda.
- Logs.
- Alertas configurables.

No implementar bloqueos que puedan utilizarse fácilmente para provocar un ataque de denegación de servicio contra usuarios legítimos.

---

# 28. CIFRADO

Todo acceso remoto debe utilizar TLS.

Nunca transmitir credenciales o tokens mediante HTTP sin cifrar en escenarios donde exista exposición externa.

Preparar:

- HTTPS.
- TLS moderno.
- Gestión de certificados.
- Renovación.

---

# 29. CIFRADO DE DATOS

El sistema debe permitir cifrado de datos en reposo cuando sea técnicamente apropiado.

Diferenciar claramente:

### Cifrado del disco

Responsabilidad del sistema operativo/filesystem.

### Cifrado de NexusCloud

Responsabilidad del propio sistema.

### Cifrado extremo a extremo

Responsabilidad del cliente.

No mezcles estos conceptos.

Diseña una arquitectura preparada para E2EE sin convertirlo en requisito obligatorio de la primera versión si aumenta innecesariamente la complejidad.

Nunca diseñes un sistema de cifrado propietario sin documentación criptográfica y revisión adecuada.

---

# 30. CLAVES Y SECRETOS

Nunca guardar:

- Passwords.
- API keys.
- Secretos.
- Tokens.

En el código fuente.

Utiliza:

- Variables de entorno.
- Secret stores cuando estén disponibles.
- Archivos de secretos con permisos apropiados.

Nunca introducir secretos en Git.

---

# 31. AUDITORÍA

Registrar eventos importantes.

Ejemplos:

- Login.
- Logout.
- Login fallido.
- Cambio de contraseña.
- Creación de usuario.
- Eliminación de usuario.
- Cambio de permisos.
- Subida.
- Descarga.
- Eliminación.
- Restauración.
- Compartición.
- Creación de enlace.
- Revocación.
- Cambio de configuración.
- Cambio de almacenamiento.
- Backup.
- Restauración.
- Eventos de seguridad.

---

# 32. LOGS

Logs configurables.

Configurar:

- Nivel.
- Rotación.
- Retención.
- Ubicación.
- Formato.

Niveles:

TRACE
DEBUG
INFO
WARN
ERROR
FATAL

No registrar secretos ni contenido privado de archivos.

---

# 33. BÚSQUEDA

Implementa un buscador potente.

Debe poder buscar:

- Nombre.
- Ruta.
- Tipo.
- Extensión.
- Usuario.
- Fecha.
- Tamaño.

Preparar arquitectura para búsqueda de contenido.

No indexar automáticamente todo el contenido privado si eso supone un riesgo de privacidad.

Debe ser configurable.

---

# 34. MINIATURAS

Generar miniaturas cuando sea posible.

Soportar:

- JPG.
- PNG.
- WEBP.
- GIF.
- Vídeos.
- PDF.

Arquitectura preparada para otros formatos.

Nunca ejecutar procesadores de archivos no confiables sin aislamiento y validación.

---

# 35. PREVISUALIZACIÓN

Permitir visualizar:

- Imágenes.
- PDF.
- Vídeos.
- Audio.
- Texto.
- Markdown.
- Código.

Preparar soporte para otros formatos.

---

# 36. EDITOR

Cuando sea técnicamente viable:

- TXT.
- Markdown.
- Código.

No implementar un editor Office completo desde cero.

Si se integra una herramienta externa, hacerlo mediante un módulo claramente separado y configurable.

---

# 37. COMPARTICIÓN

Sistema avanzado de compartición.

Permitir compartir:

### Usuarios

Usuario → usuario.

### Grupos

Usuario → grupo.

### Enlaces

Crear enlaces públicos.

Opciones:

- Solo lectura.
- Lectura/escritura.
- Subida.
- Descarga.
- Contraseña.
- Fecha de expiración.
- Límite de descargas.
- Límite de tamaño.
- Revocación.
- Nombre personalizado.

---

# 38. SUBIDA ANÓNIMA

Permitir enlaces de subida sin acceso al resto del contenido cuando el administrador lo autorice.

Debe estar:

- Desactivado por defecto.
- Protegido contra abuso.
- Limitado.
- Auditado.

---

# 39. SINCRONIZACIÓN

Implementar un Sync Engine.

Modos:

- Manual.
- Automático.
- Bajo demanda.

Cliente:

Windows
Linux
Android

Debe permitir seleccionar carpetas sincronizadas.

Ejemplo:

PC:

C:\Users\Ivan\NexusCloud

↓

NexusCloud

↓

/Documentos

---

# 40. CONFLICTOS

Gestionar correctamente:

Archivo modificado localmente.

Archivo modificado remotamente.

No sobrescribir silenciosamente.

Crear versiones/conflict copies cuando corresponda.

---

# 41. REANUDACIÓN

Las subidas y descargas grandes deben poder reanudarse.

Si una transferencia se interrumpe:

No empezar desde cero innecesariamente.

Implementar:

- Chunked upload.
- Resumable upload.
- Checkpoints.
- Integridad por chunks.

---

# 42. API

Implementar una API REST profesional.

Versionada:

/api/v1/

Nunca romper una API sin migración.

Documentación:

OpenAPI.

Preparar SDKs futuros.

---

# 43. WEBDAV

Implementar WebDAV como módulo independiente.

Debe poder:

- Activarse/desactivarse.
- Configurarse.
- Limitarse.
- Auditarse.

Desactivado por defecto si no es necesario.

---

# 44. INTERFAZ WEB

Crear una interfaz web moderna.

Pero debe poder desactivarse completamente.

La interfaz debe parecerse más a:

Windows Explorer

que a una interfaz excesivamente minimalista.

Elementos:

- Árbol lateral.
- Carpetas.
- Archivos.
- Breadcrumbs.
- Vista lista.
- Vista iconos.
- Ordenación.
- Filtros.
- Menús contextuales.
- Drag & drop.
- Subida.
- Descarga.
- Compartición.

---

# 45. INTERFAZ DE ESCRITORIO

Windows y Linux.

Debe poder:

- Iniciar sesión.
- Explorar archivos.
- Subir.
- Descargar.
- Sincronizar.
- Compartir.
- Ver estado de sincronización.
- Gestionar carpetas sincronizadas.

---

# 46. ANDROID

Aplicación Android.

Debe permitir:

- Login.
- Exploración.
- Subida.
- Descarga.
- Compartición.
- Fotos.
- Vídeos.
- Descarga offline opcional.
- Sincronización opcional.
- Gestión de archivos.

Preparar integración futura con:

- Android Storage Access Framework.
- Share Sheet.
- Camera.
- Media picker.

---

# 47. WEB DESACTIVABLE

Debe existir una opción:

Web Interface:

ENABLED

o

DISABLED

Cuando esté desactivada:

- El servidor no debe servir la interfaz web.
- No debe abrir endpoints innecesarios.
- La API debe seguir funcionando únicamente si está habilitada.

---

# 48. PUERTOS

Nunca asumir puertos fijos.

Toda la configuración debe ser modificable.

Ejemplo:

NexusCloud API:

8080

pero el administrador podría cambiarlo a:

9000

o:

12345

etc.

Nunca asumir que 8080 estará disponible.

---

# 49. DOMINIOS

Preparar desde el principio para:

cloud.example.com

workspace.example.com

photos.example.com

notes.example.com

keys.example.com

etc.

No asumir ningún dominio concreto.

---

# 50. REVERSE PROXY

NexusCloud NO debe necesitar gestionar directamente todo el tráfico público.

Debe funcionar detrás de:

- Nginx.
- Caddy.
- Traefik.
- Otros reverse proxies compatibles.

Arquitectura:

Internet
   ↓
Firewall
   ↓
Reverse Proxy
   ↓
NexusCloud
   ↓
Storage

El reverse proxy será responsable de:

- HTTPS.
- Certificados.
- Routing.
- Subdominios.

NexusCloud debe conocer correctamente:

- IP original.
- Protocolo original.
- Host.
- Headers proxy.

Pero nunca confiar ciegamente en headers enviados por clientes externos.

Debe existir configuración de proxies de confianza.

---

# 51. CONVIVENCIA CON OTROS PROYECTOS NEXUS

Este punto es crítico.

NexusCloud debe coexistir con:

NexusWorkspace
NexusKeys
NexusPhotos
NexusNotes
NexusChat
NexusMail
etc.

Cada servicio puede tener un puerto interno independiente.

Ejemplo:

NexusCloud → 8080
NexusWorkspace → 8081
NexusKeys → 8082
NexusPhotos → 8083
NexusNotes → 8084

Pero los usuarios accederán mediante:

cloud.example.com
workspace.example.com
keys.example.com
photos.example.com
notes.example.com

Nunca asumir que estos puertos están disponibles.

Todo configurable.

---

# 52. NEXUS ECOSYSTEM

Diseña NexusCloud como núcleo del ecosistema Nexus.

Pero NO acoples las aplicaciones.

Cada aplicación debe poder funcionar independientemente.

NexusCloud ofrecerá servicios mediante APIs:

- Authentication.
- Users.
- Permissions.
- Storage.
- File access.
- Sharing.
- Notifications.
- Audit.
- API.

---

# 53. INTEGRACIÓN CON NEXUSWORKSPACE

NexusWorkspace ya existe y está prácticamente terminado.

Por tanto, NexusCloud debe prepararse específicamente para poder integrarse posteriormente con NexusWorkspace.

Ejemplo:

NexusWorkspace

↓

NexusCloud API

↓

/Workspace

No asumas que NexusWorkspace debe depender obligatoriamente de NexusCloud.

Debe poder utilizar almacenamiento local cuando NexusCloud no esté disponible.

---

# 54. PLUGIN SYSTEM

Implementa una arquitectura de plugins/extensiones.

Ejemplos futuros:

NexusPhotos
NexusNotes
NexusChat
NexusMail
NexusMedia

Los plugins no deben poder comprometer fácilmente el Core.

Define:

- API.
- Permisos.
- Versiones.
- Lifecycle.
- Activación.
- Desactivación.
- Configuración.

---

# 55. CONFIGURACIÓN

Esta debe ser una de las partes más importantes del proyecto.

NO obligues al usuario a modificar código fuente.

Todo debe configurarse desde:

- Panel de administración.
- Archivo de configuración.
- Variables de entorno cuando sea apropiado.
- CLI.

Configuraciones:

### General

- Nombre.
- Puerto.
- Host.
- Idioma.
- Zona horaria.

### Base de datos

- SQLite.
- PostgreSQL.
- MariaDB/MySQL.

### Storage

- Ubicación.
- Pools.
- Discos.
- Cuotas.
- Políticas.

### Seguridad

- 2FA.
- Sesiones.
- Contraseñas.
- Rate limiting.
- TLS.
- Políticas.

### Web

- Activada/desactivada.
- Host.
- Puerto.
- CORS.
- Cookies.
- Headers.

### API

- Activada/desactivada.
- Puerto.
- Rate limit.
- Tokens.

### WebDAV

- Activado/desactivado.
- Puerto/ruta.

### Backup

- Horario.
- Destino.
- Retención.

### Versionado

- Activado/desactivado.
- Retención.

### Papelera

- Activada/desactivada.
- Retención.

---

# 56. ADMINISTRACIÓN

Crear un panel de administración profesional.

Dashboard:

- Usuarios.
- Almacenamiento.
- CPU.
- RAM.
- Estado del sistema.
- Backups.
- Alertas.
- Seguridad.
- Servicios.

---

# 57. MONITORIZACIÓN

Mostrar:

- CPU.
- RAM.
- Disco.
- I/O.
- Red.
- Transferencias.
- Usuarios conectados.
- Sesiones.
- Errores.

Preparar integración futura con:

- Prometheus.
- Grafana.

Sin hacerlos obligatorios.

---

# 58. ALERTAS

Sistema de alertas.

Ejemplos:

- Disco lleno.
- RAID degradado.
- Backup fallido.
- Login sospechoso.
- Demasiados intentos.
- Certificado próximo a caducar.
- Error de almacenamiento.
- Integridad comprometida.

---

# 59. ACTUALIZACIONES

Diseñar un sistema de actualización seguro.

Nunca actualizar destruyendo datos.

Antes de actualizar:

- Verificar versión.
- Ejecutar migraciones.
- Crear backup cuando sea posible.
- Comprobar compatibilidad.
- Registrar operación.

Preparar rollback cuando sea viable.

---

# 60. INSTALACIÓN

Debe existir una experiencia de instalación sencilla.

Windows:

Installer.

Linux:

Binary.

Package cuando sea viable.

Docker:

Dockerfile.

Docker Compose.

Raspberry:

Compatible con ARM64 y ARM cuando sea razonable.

---

# 61. DOCKER

Crear:

Dockerfile

docker-compose.yml

Permitir:

- NexusCloud.
- PostgreSQL opcional.
- Reverse proxy opcional.

No obligar a utilizar Docker.

---

# 62. WINDOWS

Debe funcionar correctamente en Windows.

Considerar:

- Windows Service.
- Rutas Windows.
- Permisos NTFS.
- Volúmenes.
- Storage Spaces.

---

# 63. LINUX

Debe ser plataforma principal de servidor a largo plazo.

Preparar:

- systemd.
- permisos Unix.
- mount points.
- ext4.
- XFS.
- Btrfs.
- ZFS cuando corresponda.

---

# 64. RASPBERRY PI

Debe poder ejecutarse en Raspberry Pi.

Optimizar:

- RAM.
- CPU.
- I/O.
- almacenamiento.

Evitar procesos innecesarios.

---

# 65. NAS

No es obligatorio crear aplicaciones específicas para todos los NAS.

Pero la arquitectura debe ser suficientemente estándar para poder ejecutarse mediante:

- Docker.
- Linux.
- ARM64.

Documentar compatibilidad.

---

# 66. RED LOCAL

La instalación inicial debe funcionar perfectamente sin Internet.

Ejemplo:

192.168.1.100:8080

No exigir dominio.

No exigir HTTPS público.

Para LAN, permitir configurar HTTPS interno si el administrador lo desea.

---

# 67. INTERNET

Cuando se publique:

Internet
↓
Firewall
↓
Reverse Proxy
↓
HTTPS
↓
NexusCloud

Debe existir documentación completa para hacerlo de forma segura.

Nunca recomendar simplemente abrir puertos arbitrarios hacia NexusCloud sin explicar la arquitectura de seguridad.

---

# 68. FIREWALL

No modificar automáticamente el firewall del sistema sin consentimiento explícito.

Mostrar qué puertos son necesarios.

Permitir generar instrucciones de firewall.

---

# 69. CORS

CORS estricto.

Nunca:

Access-Control-Allow-Origin: *

por defecto.

Permitir configurar orígenes autorizados.

---

# 70. CSRF

Proteger las operaciones sensibles contra CSRF cuando corresponda.

---

# 71. XSS

Todo contenido generado por usuarios debe tratarse como no confiable.

Escapar y sanitizar.

---

# 72. SQL INJECTION

Usar consultas parametrizadas.

Nunca concatenar directamente entrada de usuario en SQL.

---

# 73. PATH TRAVERSAL

Protección absoluta contra:

../

y variantes.

Nunca permitir que una petición pueda escapar del Storage Root autorizado.

---

# 74. SYMLINKS

Tratar symlinks cuidadosamente.

Nunca permitir que un symlink permita acceder a archivos fuera del almacenamiento autorizado.

Configurable según plataforma.

---

# 75. UPLOAD SECURITY

Validar:

- Tamaño.
- Extensión.
- MIME.
- Contenido cuando sea apropiado.
- Nombre.
- Ruta.

No confiar únicamente en extensión.

---

# 76. ARCHIVOS MALICIOSOS

Preparar arquitectura para integración opcional con antivirus/antimalware.

Por ejemplo:

ClamAV

pero no hacerlo obligatorio.

---

# 77. RATE LIMITING

Rate limit configurable para:

- Login.
- API.
- Descargas.
- Subidas.
- Comparticiones públicas.

Diferenciar usuarios autenticados y no autenticados.

---

# 78. API TOKENS

Permitir tokens específicos para aplicaciones.

Cada token:

- Nombre.
- Permisos.
- Expiración.
- Fecha creación.
- Último uso.
- Revocación.

Nunca mostrar el token completo después de crearlo.

---

# 79. PERMISOS DE ARCHIVOS

Implementar:

- Owner.
- Read.
- Write.
- Delete.
- Share.
- Download.
- Upload.

Preparar permisos más avanzados.

---

# 80. PRIVACIDAD

No enviar datos a terceros.

No incorporar:

- Analytics externos.
- Telemetría obligatoria.
- Tracking.
- Publicidad.

Si en el futuro existe telemetría opcional, debe estar:

- Desactivada por defecto.
- Documentada.
- Configurable.

---

# 81. OFFLINE

NexusCloud debe funcionar completamente offline en LAN.

No depender de:

- Google.
- Microsoft.
- Cloudflare.
- Firebase.
- AWS.
- Servicios externos.

---

# 82. INTERNACIONALIZACIÓN

Preparar:

- Español.
- Inglés.

La arquitectura debe permitir añadir idiomas.

---

# 83. UI/UX

Diseño profesional.

Inspiración conceptual:

- Windows Explorer.
- Synology DSM.
- Google Drive.

Pero sin copiar interfaces propietarias.

Debe ser:

- Limpio.
- Rápido.
- Profesional.
- Fácil de utilizar.
- Responsive.
- Accesible.

---

# 84. TEMA

Soportar:

- Light.
- Dark.
- System.

---

# 85. DRAG & DROP

Implementar:

Arrastrar archivos.

Arrastrar carpetas.

Subir.

Mover.

Compartir.

---

# 86. PAPELERA VISUAL

Permitir:

- Restaurar.
- Eliminar definitivamente.
- Vaciar.
- Ordenar.

---

# 87. FAVORITOS

Permitir marcar:

- Archivos.
- Carpetas.

---

# 88. ACTIVIDAD

Mostrar actividad reciente.

Ejemplo:

Ivan subió:

documento.pdf

Hace 5 minutos.

---

# 89. NOTIFICACIONES

Sistema interno de notificaciones.

Preparar:

- Push Android.
- Desktop notifications.
- Email opcional.

Email debe ser opcional y configurable.

---

# 90. DOCUMENTACIÓN

Crear documentación profesional.

README.md

docs/

architecture.md

security.md

storage.md

backup.md

deployment.md

windows.md

linux.md

raspberry.md

docker.md

api.md

webdav.md

reverse-proxy.md

troubleshooting.md

development.md

contributing.md

LICENSE

---

# 91. LICENCIA

Usar:

MIT License.

Crear LICENSE correctamente.

---

# 92. TESTING

No consideres terminado el proyecto si solamente compila.

Crear:

### Unit tests

Para:

- Auth.
- Storage.
- Permissions.
- API.
- Security.
- Versioning.
- Backup.

### Integration tests

Para:

- Database.
- Storage.
- API.
- Authentication.
- Sharing.

### End-to-end tests

Para los flujos principales.

---

# 93. TESTS DE SEGURIDAD

Crear pruebas específicas para:

- SQL injection.
- Path traversal.
- XSS.
- CSRF.
- Brute force.
- Token theft.
- Invalid permissions.
- Privilege escalation.
- Symlink attacks.
- Malicious uploads.
- Session fixation.
- Expired tokens.

---

# 94. FAIL SAFE

Cuando NexusCloud detecte una situación peligrosa:

No continuar silenciosamente.

Mostrar:

- Error.
- Causa.
- Riesgo.
- Acción recomendada.

Nunca ocultar errores críticos.

---

# 95. CORRUPCIÓN

Si se detecta corrupción de datos:

- No sobrescribir automáticamente.
- Registrar.
- Alertar.
- Intentar recuperar desde backup/versiones cuando proceda.

---

# 96. MIGRACIÓN

Preparar herramientas para:

- Importar archivos.
- Exportar archivos.
- Migrar instalaciones.
- Cambiar Storage Pool.
- Cambiar base de datos.

---

# 97. EXPORTACIÓN

El usuario debe poder recuperar sus datos sin depender eternamente de NexusCloud.

Debe ser posible exportar:

- Archivos.
- Carpetas.
- Metadatos cuando sea razonable.
- Configuración.
- Usuarios mediante procedimientos seguros.

Nunca crear vendor lock-in intencionadamente.

---

# 98. CLI

Crear CLI:

nexuscloud

Ejemplos conceptuales:

nexuscloud start

nexuscloud stop

nexuscloud status

nexuscloud users

nexuscloud storage

nexuscloud backup

nexuscloud config

nexuscloud doctor

nexuscloud migrate

nexuscloud security

No tienes que utilizar exactamente estos comandos si existe una estructura CLI mejor.

---

# 99. NEXUSCLOUD DOCTOR

Crear herramienta de diagnóstico.

Debe comprobar:

- Base de datos.
- Storage.
- Permisos.
- Espacio.
- Integridad.
- Configuración.
- Puertos.
- TLS.
- Dependencias.
- Backups.
- Seguridad.

Ejemplo:

nexuscloud doctor

Resultado:

PASS Database
PASS Storage
WARNING Backup
PASS Permissions
WARNING HTTPS disabled
PASS Configuration

---

# 100. CONFIGURACIÓN SEGURA

Crear un archivo de configuración de ejemplo.

No incluir secretos reales.

Ejemplo conceptual:

config.example.yaml

Debe estar ampliamente documentado.

---

# 101. PRINCIPIO DE COMPATIBILIDAD

No asumir que todos los sistemas tienen las mismas capacidades.

La arquitectura debe utilizar:

Capability Detection.

Ejemplo:

Windows:

RAID via Storage Spaces.

Linux:

mdadm.

Btrfs:

Snapshots.

ZFS:

Snapshots.

Si una función no existe:

Mostrar:

"Esta funcionalidad no está disponible en esta plataforma."

No fingir soporte.

---

# 102. ALMACENAMIENTO MULTIPLATAFORMA

Separar:

Storage abstraction layer

de

OS-specific storage provider.

Ejemplo:

StorageProvider

WindowsStorageProvider

LinuxStorageProvider

DockerStorageProvider

etc.

---

# 103. SEGURIDAD DEL SERVIDOR

El backend debe ejecutarse con el mínimo privilegio posible.

En Linux:

No ejecutar como root salvo operaciones que realmente lo necesiten.

En Windows:

Utilizar una cuenta de servicio con permisos mínimos cuando sea viable.

---

# 104. SEGURIDAD DE DOCKER

El contenedor no debe utilizar:

privileged: true

salvo que exista una razón absolutamente necesaria.

No montar:

/

del host.

Utilizar volúmenes específicos.

---

# 105. DEPENDENCIAS

Mantener dependencias al mínimo.

Revisar vulnerabilidades.

Documentar dependencias.

No utilizar librerías abandonadas.

---

# 106. SUPPLY CHAIN SECURITY

Preparar:

- Dependabot/Renovate.
- SBOM.
- Verificación de dependencias.
- Checksums.
- Firmado de releases cuando sea viable.

---

# 107. RELEASES

Versionado semántico:

MAJOR.MINOR.PATCH

Ejemplo:

1.0.0

Preparar:

- Changelog.
- Releases.
- Migraciones.
- Compatibilidad.

---

# 108. GIT

Crear:

.gitignore

GitHub Actions o CI equivalente.

Checks:

- Build.
- Tests.
- Lint.
- Security scan.

---

# 109. CI/CD

Preparar pipelines para:

- Windows.
- Linux.
- ARM64.
- Android.
- Docker.

No publicar automáticamente releases de producción sin validación.

---

# 110. OBSERVABILIDAD

Preparar interfaces para:

- Logs.
- Metrics.
- Health checks.

Endpoints:

/health

/ready

cuando la API esté habilitada.

No revelar información sensible.

---

# 111. HEALTH CHECK

Debe indicar:

- Aplicación.
- Base de datos.
- Storage.
- Dependencias.

Pero no revelar:

- Passwords.
- Tokens.
- Rutas sensibles.
- Información innecesaria.

---

# 112. API DOCUMENTATION

Generar:

OpenAPI.

Debe permitir posteriormente generar:

- SDK.
- Cliente externo.
- Integraciones Nexus.

---

# 113. FUTURAS APLICACIONES

La arquitectura debe dejar preparado:

NexusPhotos

NexusNotes

NexusChat

NexusMail

NexusMedia

NexusMonitor

etc.

No implementar estas aplicaciones ahora.

Solo preparar las interfaces necesarias.

---

# 114. NEXUS IDENTITY

Aunque no sea obligatorio implementar un Identity Provider completo inicialmente, diseña Authentication para que en el futuro NexusCloud pueda convertirse en el proveedor central de identidad del ecosistema Nexus.

Ejemplo:

NexusWorkspace
        ↓
NexusCloud Identity
        ↓
Authentication

Pero siempre permitiendo funcionamiento independiente.

---

# 115. SSO FUTURO

Preparar arquitectura para:

OAuth2 / OpenID Connect

sin necesidad de implementarlo completamente en la primera versión si compromete el alcance.

---

# 116. DOMINIOS FUTUROS

Preparar:

cloud.midominio.com
workspace.midominio.com
keys.midominio.com
photos.midominio.com
notes.midominio.com

Sin asumir dominio concreto.

---

# 117. PUERTOS FUTUROS

No hardcodear:

8080.

Todos los puertos deben proceder de configuración.

Documentar:

Internal Port
External Port
Reverse Proxy Port

---

# 118. SEGURIDAD DEL REVERSE PROXY

Documentar específicamente:

- Trusted proxies.
- Forwarded headers.
- TLS termination.
- HSTS.
- Cookies Secure.
- SameSite.
- CSP.
- Rate limiting.
- WebSocket proxying si fuese necesario.

No confiar automáticamente en:

X-Forwarded-For

X-Forwarded-Proto

etc.

---

# 119. CSP

Implementar Content Security Policy estricta cuando sea compatible con la interfaz.

Evitar:

unsafe-inline

y

unsafe-eval

si técnicamente es posible.

---

# 120. COOKIES

Cookies:

- Secure.
- HttpOnly.
- SameSite apropiado.

No almacenar tokens sensibles en lugares inseguros del cliente.

---

# 121. MOBILE SECURITY

Android:

- Keystore.
- Secure storage.
- Biometría cuando sea apropiado.
- Protección de tokens.
- No guardar credenciales en texto plano.

---

# 122. DESKTOP SECURITY

Windows/Linux:

- Secure credential storage cuando sea posible.
- No guardar tokens sin cifrar.
- Bloqueo de sesión local opcional.

---

# 123. COMPARTICIONES PÚBLICAS

Los enlaces públicos deben utilizar identificadores aleatorios criptográficamente seguros.

Nunca:

/share/1

/ share/2

etc.

No permitir enumeración.

---

# 124. PASSWORD SHARING

Las contraseñas de enlaces deben almacenarse de manera segura.

Nunca almacenar contraseñas de compartición en texto plano.

---

# 125. PRIVILEGE ESCALATION

Auditar especialmente:

- User → Admin.
- User → otro usuario.
- User → archivos privados.
- API tokens.
- Plugins.

---

# 126. ADMIN ACTIONS

Las acciones administrativas críticas deben poder requerir reautenticación.

Ejemplo:

- Eliminar usuario.
- Eliminar Storage Pool.
- Cambiar configuración de seguridad.
- Desactivar 2FA.
- Eliminar backup.
- Restaurar backup.

---

# 127. RECOVERY

Crear mecanismo seguro de recuperación administrativa.

No crear:

"master password"

que pueda descifrar absolutamente todo.

Si existe mecanismo de recuperación, documentarlo y diseñarlo con separación de privilegios.

---

# 128. RANSOMWARE

Preparar protección contra ransomware mediante:

- Versionado.
- Snapshots.
- Backups.
- Alertas.
- Detección de comportamiento anómalo cuando sea razonable.

Nunca prometer protección absoluta.

---

# 129. STORAGE MIGRATION

Permitir migrar:

Storage A

↓

Storage B

sin perder metadatos.

Mostrar progreso.

Permitir cancelar de forma segura.

Verificar integridad.

---

# 130. DUPLICADOS

Preparar detección de archivos duplicados mediante hash.

No activar deduplicación física automáticamente.

Primero analizar riesgos y rendimiento.

---

# 131. CACHE

Implementar cache únicamente donde sea beneficioso.

Debe ser:

- Configurable.
- Limitable.
- Eliminable.

Nunca utilizar cache como almacenamiento permanente.

---

# 132. THUMBNAIL CACHE

Separar cache de miniaturas del almacenamiento principal.

Permitir:

- Tamaño máximo.
- Limpieza.
- Regeneración.

---

# 133. PERFORMANCE

Optimizar para:

- Raspberry Pi.
- PC doméstico.
- Servidor pequeño.

Pero permitir crecer hasta aproximadamente:

100 usuarios.

No optimizar prematuramente.

Medir antes de complicar.

---

# 134. CONCURRENCIA

Gestionar correctamente:

- Subidas simultáneas.
- Ediciones.
- Eliminaciones.
- Restauraciones.
- Backups.
- Sincronización.

Evitar:

- Race conditions.
- Deadlocks.
- Corrupción.

---

# 135. LOCKING

Utilizar mecanismos de locking apropiados.

Nunca bloquear globalmente todo el almacenamiento por una operación pequeña.

---

# 136. LARGE FILES

Soportar archivos grandes.

No cargar archivos completos en RAM.

Utilizar streaming.

---

# 137. STREAMING

Descargas y subidas deben utilizar streaming siempre que sea apropiado.

---

# 138. SEGURIDAD DE PREVIEWS

Los procesadores de:

PDF
Vídeo
Imagen
Office

deben tratarse como superficies de ataque.

Aislar cuando sea necesario.

---

# 139. NO TELEMETRÍA

NexusCloud no debe enviar información a servidores externos por defecto.

---

# 140. FIRST RUN

Crear asistente de primera instalación.

Pasos:

1. Idioma.
2. Crear administrador.
3. Elegir almacenamiento.
4. Configurar base de datos.
5. Configurar seguridad.
6. Revisar configuración.
7. Finalizar.

Nunca crear admin/admin.

---

# 141. SECURITY CHECKUP

Después de instalar:

Security Check

Mostrar:

PASS
WARNING
CRITICAL

Ejemplos:

2FA enabled
HTTPS configured
Default settings secure
Backup configured
Storage healthy
WebDAV disabled
Public registration disabled

---

# 142. ADMIN DASHBOARD

Debe ser extremadamente claro.

Ejemplo:

SYSTEM HEALTH
████████████ 100%

STORAGE
2.4 TB / 4 TB

USERS
6 / 100

BACKUP
Last backup: 02:00

SECURITY
No critical issues

---

# 143. USER DASHBOARD

Usuario normal:

- Mis archivos.
- Compartido conmigo.
- Compartido por mí.
- Favoritos.
- Recientes.
- Papelera.
- Cuota.

---

# 144. RESPONSIVE

Web adaptable:

Desktop
Tablet
Mobile

---

# 145. ACCESIBILIDAD

Cumplir buenas prácticas WCAG cuando sea razonable.

Soportar:

- Keyboard navigation.
- Screen readers.
- Contraste.
- Focus.
- Labels.

---

# 146. NO SOBRECARGAR LA INTERFAZ

Mostrar información avanzada únicamente cuando sea necesaria.

El usuario normal no debe ver:

- RAID.
- Storage pools.
- Logs.
- API.
- TLS.
- etc.

El administrador sí.

---

# 147. PERFILES DE CONFIGURACIÓN

Preparar:

Personal
Family
Small Business
Advanced

como presets opcionales.

Pero siempre permitiendo personalización manual.

---

# 148. CONFIGURACIÓN AVANZADA

Permitir acceder a todas las opciones desde:

Advanced Settings.

Nunca ocultar una opción importante de manera permanente.

---

# 149. EXPORTAR CONFIGURACIÓN

Permitir exportar configuración sin secretos.

Ejemplo:

nexuscloud-config.json

---

# 150. IMPORTAR CONFIGURACIÓN

Permitir importar configuración validando:

- Schema.
- Compatibilidad.
- Seguridad.

Nunca sobrescribir automáticamente una instalación sin confirmación.

---

# 151. CONFIG SCHEMA

Crear un esquema de configuración versionado.

Ejemplo:

configVersion: 1

Cuando cambie:

configVersion: 2

Implementar migraciones.

---

# 152. VALIDACIÓN

Antes de aplicar una configuración:

Validar.

Si es incorrecta:

NO aplicarla.

Mostrar exactamente qué está mal.

---

# 153. CONFIG HOT RELOAD

Implementar hot reload únicamente para configuraciones seguras.

Para cambios que requieran reinicio:

Mostrar:

"Restart required."

---

# 154. DATA DIRECTORY

Nunca asumir:

C:\NexusCloud

ni:

/var/nexuscloud

Permitir configurar:

Data directory.

---

# 155. SEPARACIÓN DE DATOS

Separar:

Application

Config

Database

Storage

Cache

Logs

Backups

---

# 156. EJEMPLO

Conceptualmente:

/nexuscloud

/config
/data
/database
/cache
/logs
/backups

/storage

Pero permitir que cada uno esté en diferentes discos.

---

# 157. STORAGE DISTRIBUIDO FUTURO

No implementarlo inicialmente.

Pero diseñar interfaces que permitan en el futuro:

S3-compatible storage
MinIO
Remote NexusCloud
etc.

---

# 158. FEDERACIÓN FUTURA

No implementar ahora.

Pero preparar API para que en el futuro:

NexusCloud A

pueda compartir recursos con:

NexusCloud B.

---

# 159. MULTI-TENANCY

No implementarlo inicialmente.

Pero evitar decisiones arquitectónicas que hagan imposible soportarlo en el futuro.

---

# 160. 100 USUARIOS

La arquitectura debe estar diseñada para aproximadamente 100 usuarios simultáneos/máximos.

No necesitas diseñar una plataforma para millones de usuarios.

Prioriza estabilidad y seguridad.

---

# 161. NO SOBREINGENIERÍA

No implementes una característica solamente porque sea técnicamente posible.

Cada componente debe justificar:

- Seguridad.
- Rendimiento.
- Mantenibilidad.
- Utilidad.

---

# 162. PRINCIPIO DE SIMPLICIDAD

Si existen dos soluciones:

A: extremadamente compleja.

B: sencilla, segura y mantenible.

Elegir B salvo que exista una razón técnica importante para elegir A.

---

# 163. DESARROLLO POR FASES

No intentes crear absolutamente todo en un único paso sin comprobar nada.

Divide el desarrollo en fases.

### FASE 1

Core
Configuración
Database
Users
Authentication
Storage
API
Security foundation

### FASE 2

Web UI
File manager
Sharing
Trash
Versioning

### FASE 3

Desktop
Windows
Linux

### FASE 4

Android

### FASE 5

Backup
Snapshots
Storage management

### FASE 6

Advanced security
2FA
Passkeys
WebDAV

### FASE 7

Nexus ecosystem integration

---

# 164. REGLA DURANTE EL DESARROLLO

Antes de escribir código:

Analiza el proyecto.

Diseña arquitectura.

Comprueba dependencias.

Comprueba compatibilidad multiplataforma.

Identifica riesgos de seguridad.

Después implementa.

No empieces escribiendo cientos de archivos sin arquitectura.

---

# 165. DOCUMENTAR DECISIONES

Crear:

docs/architecture/decisions/

Registrar decisiones importantes mediante ADR.

Ejemplo:

ADR-001-backend-go.md

ADR-002-storage.md

ADR-003-database.md

ADR-004-authentication.md

---

# 166. SECURITY THREAT MODEL

Crear:

docs/security/threat-model.md

Analizar:

- Attacker externo.
- Usuario malicioso.
- Usuario comprometido.
- Admin comprometido.
- Malware local.
- Ransomware.
- Robo de dispositivo.
- Filtración de backup.
- Ataques internos.

---

# 167. SECURITY REVIEW

Antes de considerar una fase terminada:

Realizar revisión de seguridad.

Preguntarte:

¿Puede un usuario acceder a archivos de otro?

¿Puede escapar del Storage Root?

¿Puede escalar privilegios?

¿Puede enumerar usuarios?

¿Puede reutilizar tokens?

¿Puede descargar archivos sin permiso?

¿Puede abusar de enlaces públicos?

¿Puede provocar DoS?

¿Se filtran secretos?

---

# 168. PRINCIPIO ZERO TRUST

No confiar automáticamente en:

- Clientes.
- IPs.
- Usuarios.
- Headers.
- Plugins.
- Archivos.
- Storage providers.

Validar cada operación.

---

# 169. SECURE DEFAULT CONFIG

La configuración inicial debe ser:

- Sin registro público.
- Sin WebDAV.
- Sin enlaces públicos.
- Sin API pública innecesaria.
- Rate limiting activo.
- Sesiones seguras.
- Logs activos.
- Auditoría activa.
- Backup recomendado.
- 2FA disponible.
- Web pública opcional.

---

# 170. ERROR MESSAGES

No revelar información interna.

Incorrecto:

"SQL query failed: SELECT..."

Correcto:

"Unable to complete operation."

Registrar detalles internamente.

---

# 171. DEBUG

Nunca activar DEBUG automáticamente en producción.

---

# 172. SECRETS

Nunca mostrar secretos en:

- Logs.
- Errores.
- Dashboard.
- API responses.

---

# 173. BACKUP SECURITY

Los backups deben poder cifrarse.

Permitir:

- Password/key.
- Rotación.
- Retención.

No eliminar automáticamente backups sanos simplemente porque exista uno nuevo.

---

# 174. RESTORE

Antes de restaurar:

Mostrar:

- Fecha.
- Tamaño.
- Estado.
- Origen.

Permitir restauración selectiva cuando sea viable.

---

# 175. HEALTH MONITORING

Implementar:

/health

/ready

y métricas opcionales.

---

# 176. SECURITY EVENTS

Separar:

Application Logs

de

Security Audit Logs.

---

# 177. AUDIT IMMUTABILITY

Cuando sea posible, los logs de auditoría críticos deben ser difíciles de alterar por usuarios normales.

---

# 178. TIME

Utilizar UTC internamente.

Convertir a zona local en UI.

---

# 179. FILE NAMES

Soportar correctamente:

Unicode.

Español.

Caracteres internacionales.

Windows-invalid characters.

Linux differences.

---

# 180. CROSS PLATFORM PATH

Nunca construir rutas concatenando strings manualmente.

Utilizar APIs seguras del lenguaje.

---

# 181. WINDOWS PATHS

Gestionar correctamente:

C:\

UNC

Network drives

Long paths.

---

# 182. LINUX PATHS

Gestionar:

Permissions
UID
GID
Mount points
Symlinks

---

# 183. STORAGE PERMISSIONS

No cambiar permisos del sistema operativo sin consentimiento.

---

# 184. DATA LOSS PREVENTION

Operaciones destructivas deben:

- Solicitar confirmación cuando sea apropiado.
- Mostrar consecuencias.
- Registrar operación.

---

# 185. DELETE

Para:

Delete permanently

mostrar advertencia clara.

---

# 186. ADMIN DELETE

Eliminar:

Storage Pool
Database
Backup

requiere confirmación reforzada.

---

# 187. API VERSIONING

Nunca:

/api/

sin versionar a largo plazo.

Utilizar:

/api/v1/

---

# 188. CLIENT COMPATIBILITY

El servidor debe conocer versión del cliente.

Permitir:

- Minimum supported version.
- Recommended version.
- Incompatible version.

---

# 189. UPDATE POLICY

Nunca bloquear inmediatamente clientes antiguos sin una política clara.

---

# 190. DEVELOPMENT MODE

Debe existir:

Development

Production

Test

pero producción debe ser segura por defecto.

---

# 191. SECURITY HEADERS

Configurar cuando proceda:

HSTS
CSP
X-Content-Type-Options
Referrer-Policy
Permissions-Policy
Frame protections

Utilizar headers modernos y apropiados.

---

# 192. FILE DOWNLOADS

Configurar correctamente:

Content-Disposition

Content-Type

Evitar MIME sniffing peligroso.

---

# 193. PUBLIC FILES

Los archivos públicos deben pasar por un mecanismo controlado.

No exponer directamente:

/storage/...

al servidor web.

---

# 194. STORAGE ROOT

El servidor web/API nunca debe poder servir arbitrariamente cualquier archivo del sistema.

---

# 195. INTERNAL FILE ACCESS

Implementar una abstracción:

FileService

que controle:

- Authentication.
- Authorization.
- Path.
- Storage.
- Logging.

---

# 196. NO DIRECT STORAGE ACCESS

Los módulos no deben acceder directamente al filesystem sin pasar por las interfaces correspondientes salvo operaciones internas justificadas.

---

# 197. API SECURITY

Todas las operaciones deben comprobar:

Authentication

+

Authorization

+

Resource ownership/permission.

---

# 198. IDOR

Prestar especial atención a:

Insecure Direct Object Reference.

Nunca permitir:

/files/123

simplemente porque el usuario conozca el ID.

---

# 199. ENUMERATION

IDs de:

- Usuarios.
- Comparticiones.
- Archivos.

no deben permitir enumeración útil por atacantes.

---

# 200. FINAL REQUIREMENT

No consideres NexusCloud terminado cuando:

"funciona".

Debe considerarse terminado cuando:

- Funciona.
- Está probado.
- Está documentado.
- Es seguro.
- Es configurable.
- Es multiplataforma.
- Puede instalarse.
- Puede actualizarse.
- Puede realizar backups.
- Puede recuperarse.
- Puede integrarse con el ecosistema Nexus.
- Puede mantenerse durante años.

---

# INSTRUCCIONES FINALES PARA CLAUDE CODE

Antes de comenzar:

1. Analiza todo este documento.
2. Identifica requisitos funcionales.
3. Identifica requisitos no funcionales.
4. Identifica riesgos.
5. Diseña la arquitectura.
6. Presenta el árbol inicial del proyecto.
7. Define las decisiones tecnológicas.
8. Define las interfaces entre módulos.
9. Define el modelo de seguridad.
10. Define el sistema de almacenamiento.
11. Define la estrategia multiplataforma.
12. Define la estrategia de configuración.
13. Define la estrategia de despliegue.
14. Define la estrategia de testing.

Después comienza a implementar.

No preguntes continuamente cosas que ya están especificadas aquí.

Si existe una decisión que no está definida:

**elige tú la solución técnicamente más segura, mantenible, eficiente y preparada para el futuro.**

No elijas una tecnología simplemente porque sea popular.

No añadas dependencias innecesarias.

No simplifiques requisitos de seguridad.

No elimines funcionalidades importantes por comodidad.

Cuando una funcionalidad avanzada no pueda implementarse correctamente en la primera versión, crea la arquitectura necesaria para poder añadirla posteriormente sin romper el Core.

---

# PRIORIDAD ABSOLUTA

Cuando tengas que elegir entre:

Seguridad
vs
Comodidad

elige:

**SEGURIDAD**

Cuando tengas que elegir entre:

Integridad de datos
vs
Rendimiento

elige:

**INTEGRIDAD DE DATOS**

Cuando tengas que elegir entre:

Mantenibilidad
vs
Complejidad

elige:

**MANTENIBILIDAD**

Cuando tengas que elegir entre:

Arquitectura preparada para futuro
vs
Atajo rápido

elige:

**ARQUITECTURA PREPARADA PARA FUTURO**

---

# RESULTADO FINAL ESPERADO

Quiero terminar con un proyecto llamado:

# NEXUSCLOUD

Una plataforma:

- Open Source.
- MIT.
- Self-hosted.
- Offline-capable.
- Multiplataforma.
- Segura.
- Modular.
- Configurable.
- Escalable hasta aproximadamente 100 usuarios.
- Compatible con Windows.
- Compatible con Linux.
- Compatible con Raspberry Pi.
- Compatible con Docker.
- Cliente Android.
- Cliente Windows.
- Cliente Linux.
- Interfaz Web opcional.
- API REST.
- WebDAV opcional.
- Storage Pools.
- Gestión de almacenamiento.
- RAID mediante tecnologías del sistema.
- Backups.
- Snapshots.
- Versionado.
- Papelera.
- Compartición.
- Sincronización.
- Usuarios.
- Grupos.
- Roles.
- Cuotas.
- 2FA.
- Preparado para Passkeys/FIDO2.
- HTTPS.
- Reverse Proxy.
- Dominios.
- Subdominios.
- Auditoría.
- Logs.
- Monitoring.
- Sistema de plugins.
- API para futuras aplicaciones Nexus.
- Preparado para NexusWorkspace.
- Preparado para NexusKeys.
- Preparado para NexusPhotos.
- Preparado para NexusNotes.
- Preparado para futuras aplicaciones Nexus.

Y, sobre todo:

**NexusCloud debe ser diseñado como el núcleo de almacenamiento e infraestructura del ecosistema Nexus, pero sin convertirse en un sistema cerrado o dependiente de las demás aplicaciones.**

Cada componente debe poder evolucionar independientemente.

La arquitectura debe permitir que dentro de años pueda existir:

Internet
│
▼
Firewall
│
▼
Reverse Proxy
│
├── cloud.midominio.com
│      │
│      ▼
│   NexusCloud
│
├── workspace.midominio.com
│      │
│      ▼
│   NexusWorkspace
│
├── keys.midominio.com
│      │
│      ▼
│   NexusKeys
│
├── photos.midominio.com
│      │
│      ▼
│   NexusPhotos
│
├── notes.midominio.com
│      │
│      ▼
│   NexusNotes
│
└── futuras aplicaciones Nexus

Todos los servicios deben poder coexistir sin conflictos de puertos, dominios, procesos o almacenamiento.

**Construye NexusCloud pensando no solamente en la versión 1.0, sino en cómo deberá funcionar cuando el ecosistema Nexus tenga decenas de aplicaciones.**