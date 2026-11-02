package openwrtpkg

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"embed"
	"fmt"
	"time"
)

// files: copia de deploy/openwrt/netpulse-agent/files. go:embed no puede salir
// del modulo, asi que el paquete lleva su propia copia y un test comprueba que
// sigue identica a la del repo (#1476).
//
//go:embed files/*
var files embed.FS

// PkgName es el nombre del paquete OpenWrt y de la seccion UCI que crea.
const PkgName = "netpulse-agent"

// pkgEpoch fija los mtime del tar para que dos generaciones del mismo binario
// produzcan exactamente los mismos bytes.
var pkgEpoch = time.Unix(1700000000, 0).UTC()

// OpenWrtArch traduce el GOARCH del agente al nombre de arquitectura que usan
// los repos de OpenWrt. Devuelve "" para una arquitectura no soportada.
func OpenWrtArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64_cortex-a53"
	case "arm":
		return "arm_cortex-a7_neon-vfpv4"
	case "mipsle":
		return "mipsel_24kc"
	case "mips":
		return "mips_24kc"
	}
	return ""
}

// BuildIPK construye el paquete .ipk del agente para una arquitectura, a
// partir del binario ya compilado.
//
// Desde OpenWrt 24.10 el .ipk NO es un archivo ar: es un tar.gz exterior que
// contiene ./debian-binary, ./control.tar.gz y ./data.tar.gz, comprobado
// contra un paquete real de downloads.openwrt.org. Los tres miembros llevan
// prefijo "./" y el control declara /etc/config/netpulse-agent como conffile,
// que es lo que hace que la config sobreviva a un sysupgrade.
func BuildIPK(goarch, version string, binary []byte) ([]byte, error) {
	arch := OpenWrtArch(goarch)
	if arch == "" {
		return nil, fmt.Errorf("openwrt pkg: arquitectura no soportada %q", goarch)
	}
	if len(binary) == 0 {
		return nil, fmt.Errorf("openwrt pkg: binario vacio")
	}
	if version == "" {
		version = "0.0.0"
	}
	initScript, err := files.ReadFile("files/netpulse-agent.init")
	if err != nil {
		return nil, err
	}
	configFile, err := files.ReadFile("files/netpulse-agent.config")
	if err != nil {
		return nil, err
	}
	defaults, err := files.ReadFile("files/netpulse-agent.defaults")
	if err != nil {
		return nil, err
	}

	control, err := tarGz([]tarEntry{
		{name: "./control", mode: 0o644, body: controlFile(version, arch)},
		{name: "./conffiles", mode: 0o644, body: []byte("/etc/config/netpulse-agent\n")},
		{name: "./postinst", mode: 0o755, body: postinst},
		{name: "./prerm", mode: 0o755, body: prerm},
	})
	if err != nil {
		return nil, err
	}
	data, err := tarGz([]tarEntry{
		{name: "./usr/sbin/netpulse-agent", mode: 0o755, body: binary},
		{name: "./etc/init.d/netpulse-agent", mode: 0o755, body: initScript},
		{name: "./etc/config/netpulse-agent", mode: 0o644, body: configFile},
		{name: "./etc/uci-defaults/90-netpulse-agent", mode: 0o755, body: defaults},
	})
	if err != nil {
		return nil, err
	}
	return tarGzNoDir([]tarEntry{
		{name: "./debian-binary", mode: 0o644, body: []byte("2.0\n")},
		{name: "./data.tar.gz", mode: 0o644, body: data},
		{name: "./control.tar.gz", mode: 0o644, body: control},
	})
}

// controlFile es el CONTROL/control del paquete. Depends replica el Makefile
// del repo: el agente sondea las radios con iw.
func controlFile(version, arch string) []byte {
	return []byte(fmt.Sprintf(`Package: %s
Version: %s
Depends: libc, iw
License: AGPL-3.0-only
Section: utils
Architecture: %s
Maintainer: Nacho <netpulse@cloudless.club>
Description: Native OpenWrt agent for NetPulse network monitor.
`, PkgName, version, arch))
}

// postinst arranca el servicio si ya estaba habilitado y aplica los
// uci-defaults, que migran una instalacion manual previa al formato UCI.
var postinst = []byte(`#!/bin/sh
[ -n "${IPKG_INSTROOT}" ] && exit 0
if [ -x /etc/init.d/netpulse-agent ]; then
	/etc/init.d/netpulse-agent enabled && /etc/init.d/netpulse-agent restart
fi
[ -x /etc/uci-defaults/90-netpulse-agent ] && sh /etc/uci-defaults/90-netpulse-agent
exit 0
`)

// prerm para el servicio y retira el watchdog cron de versiones anteriores.
var prerm = []byte(`#!/bin/sh
[ -n "${IPKG_INSTROOT}" ] && exit 0
if [ -x /etc/init.d/netpulse-agent ]; then
	/etc/init.d/netpulse-agent stop 2>/dev/null
	/etc/init.d/netpulse-agent disable 2>/dev/null
fi
crontab -l 2>/dev/null | grep -v netpulse-watchdog | crontab - 2>/dev/null
exit 0
`)

type tarEntry struct {
	name string
	mode int64
	body []byte
	dir  bool
}

// tarGz empaqueta entradas con la cabecera de directorio "./" delante, como
// hacen ipkg-build y apk en los paquetes reales de OpenWrt.
func tarGz(entries []tarEntry) ([]byte, error) {
	return tarGzNoDir(append([]tarEntry{{name: "./", mode: 0o755, dir: true}}, entries...))
}

// tarGzNoDir empaqueta sin cabecera de directorio (es el tar exterior del ipk,
// que solo lleva los tres miembros).
func tarGzNoDir(entries []tarEntry) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Header.ModTime = pkgEpoch
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{
			Name:    e.name,
			Mode:    e.mode,
			ModTime: pkgEpoch,
			Uid:     0,
			Gid:     0,
			Format:  tar.FormatGNU,
		}
		if e.dir {
			hdr.Typeflag = tar.TypeDir
		} else {
			hdr.Typeflag = tar.TypeReg
			hdr.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if !e.dir && len(e.body) > 0 {
			if _, err := tw.Write(e.body); err != nil {
				return nil, err
			}
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
