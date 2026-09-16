# Primeros pasos (Linux, sin entorno gráfico)

Guía de arranque, de cero a tener NexusCloud funcionando en un servidor
Linux headless — todo por línea de comandos, nada asume una sesión de
escritorio ni un navegador local.

## 1. Instalar

Desde la raíz del repositorio clonado:

```sh
sudo ./install.sh
```

Con una terminal real por delante, el propio script pregunta lo poco que
hace falta (¿interfaz web?, usuario y contraseña del administrador) y al
terminar deja NexusCloud **funcionando de verdad**: además de compilar
(instala Go si hace falta, descarga oficial de go.dev), instalar el
servicio de systemd y generar `/etc/nexuscloud/config.yaml`, también crea
ese administrador y arranca el servicio — imprime la URL a abrir en el
navegador al final. Ninguno de los pasos 2 a 5 de esta guía hace falta ya
a mano; se conservan aquí como referencia de qué ha pasado por dentro, y
para poder repetir cualquiera de ellos por separado más adelante.

¿Quieres también la interfaz web? Contesta "s" a esa pregunta (es la
opción por defecto, basta con pulsar Enter) — instala Node.js LTS si hace
falta y compila la web real. Sin ella, el binario ni siquiera la incluye
— puedes añadirla después sin reinstalar desde cero, ver
[`mantenimiento.md`](mantenimiento.md#añadir-o-quitar-la-interfaz-web).

Para el uso avanzado/scriptado de siempre (sin preguntas, pensado para
Docker/CI/aprovisionamiento automático) sigue disponible:

```sh
sudo ./install.sh --unattended              # comportamiento clásico: no crea admin, no arranca
sudo NX_ADMIN_USERNAME=admin NX_ADMIN_PASSWORD=... ./install.sh   # crea admin y arranca sin preguntar nada
sudo ./install.sh --web                     # incluye la web sin pasar por las preguntas
```

## 2. Revisar la configuración (opcional -- ya no es un paso obligatorio)

```sh
sudo -e /etc/nexuscloud/config.yaml
```

Lo que más vale la pena mirar en un primer arranque:
- `storage.dataDir`: dónde vive todo (base de datos, archivos, backups por
  defecto). Si tienes un disco grande aparte, puedes moverlo ahí antes
  incluso del primer arranque.
- `server.port`: 8080 por defecto.
- `trash.retentionDays` / `versioning.maxVersionsPerFile`: cuánto tiempo
  se conservan los archivos borrados / versiones antiguas.

No hace falta tocar nada para empezar — los valores por defecto son
seguros (sin web pública, sin registro público, rate limiting activo).

## 3. Crear el primer usuario (ya hecho por el instalador -- esto es para añadir más)

`install.sh` ya te preguntó el usuario/contraseña del administrador y lo
creó. Para un segundo usuario (o para recrear el admin si usaste
`--unattended`):

```sh
sudo -u nexuscloud nexuscloud --config /etc/nexuscloud/config.yaml admin create-user
```

Sin `--username`/`--password`, los pide de forma interactiva (la
contraseña sin eco en pantalla). El primer usuario creado en la instancia
recibe automáticamente el rol `super_admin`; nunca se permiten
contraseñas de menos de 8 caracteres.

## 4. Comprobar que todo está en orden

```sh
sudo -u nexuscloud nexuscloud --config /etc/nexuscloud/config.yaml doctor
```

Real, contra una instalación recién hecha:

```
PASS      Configuration        configVersion=1
PASS      Database             sqlite, esquema v7
PASS      Storage              /var/lib/nexuscloud/storage
PASS      Port                 0.0.0.0:8080
WARNING   HTTPS                TLS no configurado; aceptable en LAN, revisar antes de exponer a Internet (§67)
PASS      Web pública          desactivada (secure by default, §3/§47)
PASS      Public registration  desactivado
```

El `WARNING` de HTTPS es normal si NexusCloud solo va a vivir dentro de tu
red local (LAN) — solo hace falta resolverlo si vas a exponerlo a
Internet. Cualquier `FAIL` hay que arreglarlo antes de seguir.

## 5. Arrancar (ya hecho por el instalador -- esto es para pararlo/reiniciarlo)

```sh
sudo systemctl start nexuscloud
systemctl status nexuscloud
journalctl -u nexuscloud -f      # logs en vivo
```

## 6. Guardar y organizar tus primeros archivos

Ya sin necesitar la interfaz web para nada — todo esto es lo mismo que
harías arrastrando ficheros en un explorador, pero por comandos:

```sh
nexuscloud --config /etc/nexuscloud/config.yaml files mkdir --username tu-usuario /Documentos
nexuscloud --config /etc/nexuscloud/config.yaml files upload --username tu-usuario ./informe.pdf /Documentos/informe.pdf
nexuscloud --config /etc/nexuscloud/config.yaml files list --username tu-usuario /Documentos
```

Ver [`comandos.md`](comandos.md) para el resto de operaciones de archivo
(descargar, mover, borrar) y [`administracion.md`](administracion.md) para
añadir más usuarios, grupos y doble factor.

## 7. Si vas a usar el cliente de escritorio

El cliente Flutter (Windows/Linux) sincroniza una carpeta local con tu
servidor automáticamente, sin tener que subir/bajar archivos a mano cada
vez. Ver [`cliente-de-escritorio.md`](cliente-de-escritorio.md).

## Si algo no arranca

Ver [`mantenimiento.md`](mantenimiento.md) y
[`preguntas-frecuentes.md`](preguntas-frecuentes.md) — casi todo lo
recurrente (puerto ocupado, permisos, `doctor` en rojo) tiene una
respuesta directa ahí.
