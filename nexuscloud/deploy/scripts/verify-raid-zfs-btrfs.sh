#!/usr/bin/env bash
# Verifica de verdad, contra RAID/ZFS/Btrfs REALES, los adaptadores de
# internal/storage/raidinfo y internal/storage/snapshotinfo (ADR-018,
# ADR-021, ADR-022) -- hasta ahora solo probados con datos sintéticos,
# porque ni la máquina de desarrollo (Windows) ni el contenedor Docker
# usado durante el desarrollo (kernel de WSL2, sin soporte md/btrfs/zfs en
# absoluto) permiten crear ninguno de los tres de verdad.
#
# Pensado para ejecutarse en una máquina Linux real (la tuya, con un
# kernel real) -- NO dentro de Docker en Windows/WSL2, ahí no funcionará
# por la misma razón de fondo que arriba.
#
# No toca ningún disco/array/pool real que ya tengas: todo se crea sobre
# ficheros de bucle (loop devices) de usar y tirar en un directorio
# temporal, y se limpia solo al terminar (incluso si algo falla a medias,
# vía trap).
#
# Uso:
#   sudo ./verify-raid-zfs-btrfs.sh [/ruta/al/binario/nexuscloud]
#   (sin argumento, compila uno nuevo desde nexuscloud/ -- necesita Go)
set -uo pipefail

[ "$(id -u)" -eq 0 ] || { echo "hay que ejecutarlo como root (sudo)."; exit 1; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CODE_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
WORKDIR="$(mktemp -d -t nexuscloud-verify-XXXXXX)"
BIN="${1:-}"

PASS=0
FAIL=0
ok()   { echo "  OK: $1"; PASS=$((PASS+1)); }
bad()  { echo "  FALLO: $1"; FAIL=$((FAIL+1)); }
skip() { echo "  SALTADO: $1"; }

# --- limpieza, siempre, incluso si algo falla a medias -------------------
LOOP_DEVS=()
MOUNTED=()
MD_DEV=""
ZPOOL_NAME=""
cleanup() {
	echo; echo "== limpiando =="
	[ -n "$ZPOOL_NAME" ] && zpool destroy "$ZPOOL_NAME" >/dev/null 2>&1
	for m in "${MOUNTED[@]}"; do umount "$m" >/dev/null 2>&1; done
	[ -n "$MD_DEV" ] && mdadm --stop "$MD_DEV" >/dev/null 2>&1
	for d in "${LOOP_DEVS[@]}"; do losetup -d "$d" >/dev/null 2>&1; done
	rm -rf "$WORKDIR"
}
trap cleanup EXIT

# --- binario ---------------------------------------------------------------
if [ -z "$BIN" ]; then
	command -v go >/dev/null 2>&1 || { echo "no hay 'go' en el PATH y no se dio un binario -- instala Go o pasa la ruta a uno ya compilado."; exit 1; }
	echo "== compilando nexuscloud desde $CODE_DIR =="
	BIN="$WORKDIR/nexuscloud"
	( cd "$CODE_DIR" && CGO_ENABLED=0 go build -trimpath -o "$BIN" ./cmd/nexuscloud ) || { echo "go build falló"; exit 1; }
fi
[ -x "$BIN" ] || { echo "no encuentro un binario ejecutable en: $BIN"; exit 1; }
echo "  binario: $BIN"

############################################
echo; echo "## 1) RAID real (mdadm, RAID1 sobre 2 loop devices)"
############################################
if ! command -v mdadm >/dev/null 2>&1; then
	skip "mdadm no está instalado (apt install mdadm / dnf install mdadm)"
elif [ ! -e /proc/mdstat ] && ! modprobe md-mod 2>/dev/null; then
	skip "el kernel no tiene soporte md (ni /proc/mdstat ni se pudo cargar md-mod) -- normal en un kernel muy recortado, contenedor, o VM sin ese módulo"
else
	dd if=/dev/zero of="$WORKDIR/raid1.img" bs=1M count=64 status=none
	dd if=/dev/zero of="$WORKDIR/raid2.img" bs=1M count=64 status=none
	L1=$(losetup -f); losetup "$L1" "$WORKDIR/raid1.img"; LOOP_DEVS+=("$L1")
	L2=$(losetup -f); losetup "$L2" "$WORKDIR/raid2.img"; LOOP_DEVS+=("$L2")
	MD_DEV=/dev/md/nexuscloud-verify
	MDADM_OUT="$(echo y | mdadm --create "$MD_DEV" --run --level=1 --raid-devices=2 "$L1" "$L2" --metadata=1.2 2>&1)"
	if [ $? -eq 0 ]; then
		sleep 2
		echo "--- /proc/mdstat real ---"
		cat /proc/mdstat
		OUT=$("$BIN" storage raid)
		echo "--- nexuscloud storage raid ---"
		echo "$OUT"
		if echo "$OUT" | grep -qi "raid1\|mirror"; then ok "el array RAID1 real se detectó"; else bad "no se detectó el array RAID1 real"; fi

		echo "--- fallando un disco para probar 'Degraded' de verdad ---"
		mdadm --manage "$MD_DEV" --fail "$L1" >/dev/null 2>&1
		sleep 1
		OUT=$("$BIN" storage raid)
		echo "$OUT"
		if echo "$OUT" | grep -qi "degrad"; then ok "el array degradado se detectó de verdad tras fallar un disco"; else bad "no se detectó 'degraded' tras fallar un disco de verdad"; fi
	else
		skip "mdadm --create falló -- salida real:"
		echo "$MDADM_OUT" | sed 's/^/    /'
	fi
fi

############################################
echo; echo "## 2) Btrfs real (mkfs.btrfs + montar + subvolumen + snapshot reales)"
############################################
if ! command -v mkfs.btrfs >/dev/null 2>&1; then
	skip "btrfs-progs no está instalado (apt install btrfs-progs / dnf install btrfs-progs)"
elif ! grep -q btrfs /proc/filesystems 2>/dev/null && ! modprobe btrfs 2>/dev/null; then
	skip "el kernel no tiene soporte btrfs (ni en /proc/filesystems ni se pudo cargar el módulo) -- normal en un kernel recortado (p.ej. WSL2)"
else
	dd if=/dev/zero of="$WORKDIR/btrfs.img" bs=1M count=256 status=none
	L3=$(losetup -f); losetup "$L3" "$WORKDIR/btrfs.img"; LOOP_DEVS+=("$L3")
	mkfs.btrfs -q "$L3" >/dev/null 2>&1
	MNT="$WORKDIR/btrfs-mnt"
	mkdir -p "$MNT"
	if mount "$L3" "$MNT" 2>/dev/null; then
		MOUNTED+=("$MNT")
		btrfs subvolume create "$MNT/datos" >/dev/null 2>&1
		echo "contenido real" > "$MNT/datos/archivo.txt"
		btrfs subvolume snapshot -r "$MNT/datos" "$MNT/snapshot-real" >/dev/null 2>&1
		echo "--- btrfs subvolume list -s (comando real) ---"
		btrfs subvolume list -s "$MNT"
		OUT=$("$BIN" storage snapshots)
		echo "--- nexuscloud storage snapshots ---"
		echo "$OUT"
		if echo "$OUT" | grep -q "snapshot-real"; then ok "el snapshot Btrfs real se detectó"; else bad "no se detectó el snapshot Btrfs real"; fi
	else
		skip "no se pudo montar el filesystem btrfs de prueba"
	fi
fi

############################################
echo; echo "## 3) ZFS real (zpool/dataset/snapshot reales, con soporte -p si tu ZFS lo tiene)"
############################################
if ! command -v zfs >/dev/null 2>&1; then
	skip "zfs no está instalado (apt install zfsutils-linux / dnf install zfs -- necesita el módulo de kernel zfs.ko, vía DKMS en Debian/Ubuntu)"
else
	dd if=/dev/zero of="$WORKDIR/zfs.img" bs=1M count=256 status=none
	ZPOOL_NAME="nexuscloud-verify-$$"
	if zpool create "$ZPOOL_NAME" "$WORKDIR/zfs.img" >/dev/null 2>&1; then
		zfs create "$ZPOOL_NAME/datos" >/dev/null 2>&1
		echo "contenido real" > "/$ZPOOL_NAME/datos/archivo.txt" 2>/dev/null || echo "contenido real" > "$(zfs get -H -o value mountpoint "$ZPOOL_NAME/datos")/archivo.txt"
		zfs snapshot "$ZPOOL_NAME/datos@snap-real"
		echo "--- zfs list -H -p -t snapshot (el comando exacto que ejecuta el codigo Go) ---"
		zfs list -H -p -t snapshot -o name,creation 2>&1
		OUT=$("$BIN" storage snapshots)
		echo "--- nexuscloud storage snapshots ---"
		echo "$OUT"
		if echo "$OUT" | grep -q "snap-real"; then
			ok "el snapshot ZFS real se detectó"
		else
			bad "no se detectó el snapshot ZFS real"
			if zfs list -H -p -t snapshot >/dev/null 2>&1; then :; else
				echo "    posible causa real (confirmada durante el desarrollo, 2026-09-15): tu 'zfs' no soporta -p"
				echo "    ('invalid option' arriba) -- típico de zfs-fuse (proyecto antiguo, ~2011-2013)."
				echo "    NexusCloud exige -p (fechas en epoch Unix, sin ambigüedad de formato/idioma);"
				echo "    con ZFS-on-Linux moderno (zfsutils-linux + zfs-dkms) esto no ocurre."
			fi
		fi
	else
		skip "zpool create falló -- normal si el módulo zfs.ko no está cargado (modprobe zfs) o no hay soporte DKMS instalado"
	fi
fi

echo
echo "=== RESUMEN: $PASS OK, $FAIL FALLOS (saltados no cuentan como fallo -- son partes de tu sistema sin ese subsistema instalado) ==="
[ "$FAIL" -eq 0 ]
