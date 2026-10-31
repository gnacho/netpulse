package sshkey

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestEnsureKeypairRestoresFromBackup(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")

	// Generar par inicial.
	if err := EnsureKeypair(keyPath); err != nil {
		t.Fatalf("EnsureKeypair inicial falló: %v", err)
	}
	origPub, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatalf("no se pudo leer clave pública: %v", err)
	}

	// Simular rotación: RotateKeypair mueve el par actual a .bak.<epoch> y
	// genera uno nuevo. EnsureKeypair no debe restaurar ese backup porque es
	// una rotación intencionada.
	if _, err := RotateKeypair(keyPath); err != nil {
		t.Fatalf("RotateKeypair falló: %v", err)
	}
	if err := EnsureKeypair(keyPath); err != nil {
		t.Fatalf("EnsureKeypair tras rotación falló: %v", err)
	}
	newPub, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatalf("no se pudo leer nueva clave pública: %v", err)
	}
	if string(newPub) == string(origPub) {
		t.Fatalf("se esperaba un par nuevo tras rotación, pero coinciden")
	}

	// Simular pérdida: borrar par actual. EnsureKeypair debería restaurar el backup.
	if err := os.Remove(keyPath); err != nil {
		t.Fatalf("remove key failed: %v", err)
	}
	if err := os.Remove(keyPath + ".pub"); err != nil {
		t.Fatalf("remove pub failed: %v", err)
	}
	if err := EnsureKeypair(keyPath); err != nil {
		t.Fatalf("EnsureKeypair tras pérdida falló: %v", err)
	}
	restoredPub, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatalf("no se pudo leer clave pública restaurada: %v", err)
	}
	if string(restoredPub) != string(origPub) {
		t.Fatalf("la clave restaurada no coincide con el backup; got=%s want=%s", restoredPub, origPub)
	}
}

func TestRestoreLatestBackupIgnoresIncomplete(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")

	// Backup sin .pub: no debería restaurar; se genera uno nuevo.
	bakKey := keyPath + ".bak.1234567890"
	if err := os.WriteFile(bakKey, []byte("incomplete"), 0o600); err != nil {
		t.Fatalf("write incomplete backup failed: %v", err)
	}
	if err := EnsureKeypair(keyPath); err != nil {
		t.Fatalf("EnsureKeypair falló: %v", err)
	}
	if _, err := os.Stat(keyPath + ".pub"); err != nil {
		t.Fatalf("no se generó el par nuevo: %v", err)
	}
}

// TestGeneratedKeyIsOpenSSHFormatInProcess (#1701): la clave se genera en
// proceso (sin ssh-keygen) en formato OpenSSH, que es el mismo formato que
// el pool SSH puede leer. En OpenWrt 21.02 el ssh-keygen disponible es el
// de Dropbear y escribe un formato que crypto/ssh rechaza.
func TestGeneratedKeyIsOpenSSHFormatInProcess(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")

	if err := EnsureKeypair(keyPath); err != nil {
		t.Fatalf("EnsureKeypair falló: %v", err)
	}
	privPEM, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}
	block, _ := pem.Decode(privPEM)
	if block == nil {
		t.Fatal("la clave privada no es PEM")
	}
	if block.Type != "OPENSSH PRIVATE KEY" {
		t.Fatalf("tipo PEM = %q, want OPENSSH PRIVATE KEY", block.Type)
	}
	if !privateParses(keyPath) {
		t.Fatal("crypto/ssh no puede leer la clave generada en proceso")
	}
	pubRaw, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatalf("read pub: %v", err)
	}
	if !strings.Contains(string(pubRaw), "netpulse") {
		t.Fatalf("la clave pública debería llevar el comentario netpulse: %s", pubRaw)
	}
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat private: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permisos de la clave privada = %v, want 0600", info.Mode().Perm())
	}
}

// TestEnsureKeypairRegeneratesUnparseableKey (#1701): una clave existente en
// formato ilegible (como la que dejó el ssh-keygen de Dropbear en el
// GL-MT6000 del reportero: 83 bytes sin PEM) se respalda y se regenera en
// proceso en lugar de quedarse para siempre rota.
func TestEnsureKeypairRegeneratesUnparseableKey(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")

	// 83 bytes arbitrarios, no-PEM: patrón del reporte (Dropbear 21.02).
	broken := bytes.Repeat([]byte{0xde, 0xad}, 41)
	broken = append(broken, 0xbe)
	if err := os.WriteFile(keyPath, broken, 0o600); err != nil {
		t.Fatalf("write broken key: %v", err)
	}
	if err := os.WriteFile(keyPath+".pub", []byte("ssh-ed25519 AAAAtesting dropbear\n"), 0o644); err != nil {
		t.Fatalf("write broken pub: %v", err)
	}

	if err := EnsureKeypair(keyPath); err != nil {
		t.Fatalf("EnsureKeypair con clave rota falló: %v", err)
	}
	if !privateParses(keyPath) {
		t.Fatal("la clave no se regeneró en formato legible")
	}
	// El par roto queda respaldado como .bak.<epoch> (la .pub como .bak.<epoch>.pub).
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	baks := 0
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, filepath.Base(keyPath)+".bak.") && !strings.HasSuffix(name, ".pub") {
			baks++
		}
	}
	if baks != 1 {
		t.Fatalf("se esperaba 1 backup de la clave rota, hay %d", baks)
	}
	pubRaw, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatalf("read regenerated pub: %v", err)
	}
	if strings.Contains(string(pubRaw), "dropbear") {
		t.Fatalf("la clave pública rota no fue reemplazada: %s", pubRaw)
	}
}

// TestGetPublicKeyFingerprintInProcess (#1701): el fingerprint se calcula en
// proceso (SHA-256, formato OpenSSH moderno) sin depender de ssh-keygen, que
// en OpenWrt 21.02 (Dropbear) no soporta -lf y dejaba el campo vacío.
func TestGetPublicKeyFingerprintInProcess(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")

	if err := EnsureKeypair(keyPath); err != nil {
		t.Fatalf("EnsureKeypair falló: %v", err)
	}
	key := GetPublicKey(keyPath)
	if key == nil {
		t.Fatal("GetPublicKey devolvió nil")
	}
	if key.Fingerprint == "" {
		t.Fatal("fingerprint vacío: sigue dependiendo de ssh-keygen -lf")
	}
	if !strings.HasPrefix(key.Fingerprint, "SHA256:") {
		t.Fatalf("fingerprint = %q, want prefijo SHA256:", key.Fingerprint)
	}
}

// genPubKey genera una clave pública de un tipo concreto para los tests de
// merge (los tipos ed25519/ecdsa/rsa generan claves distintas entre sí).
func genPubKey(t *testing.T, kind string) ssh.PublicKey {
	t.Helper()
	var priv any
	switch kind {
	case "ed25519":
		_, p, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("generate ed25519 key: %v", err)
		}
		priv = p
	case "ecdsa":
		p, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("generate ecdsa key: %v", err)
		}
		priv = p
	case "rsa":
		p, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("generate rsa key: %v", err)
		}
		priv = p
	default:
		t.Fatalf("kind no soportado: %s", kind)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer %s: %v", kind, err)
	}
	return signer.PublicKey()
}

// lineFor construye una línea known_hosts equivalente a la que escriben
// accept-new y ssh-keyscan: "<hosts> <tipo> <base64>".
func lineFor(t *testing.T, host string, key ssh.PublicKey) string {
	t.Helper()
	return knownhosts.Line([]string{host}, key)
}

// TestPinHostKeysMergeNewHost: host desconocido (TOFU) → añade todas las keys.
func TestPinHostKeysMergeNewHost(t *testing.T) {
	ed := genPubKey(t, "ed25519")
	ec := genPubKey(t, "ecdsa")
	rs := genPubKey(t, "rsa")
	scan := []khEntry{
		{hosts: []string{"192.168.1.5"}, key: ed},
		{hosts: []string{"192.168.1.5"}, key: ec},
		{hosts: []string{"192.168.1.5"}, key: rs},
	}
	got := pinHostKeysMerge("192.168.1.5", scan, nil)
	if len(got) != 3 {
		t.Fatalf("host nuevo debería pinar 3 claves, got=%d (%v)", len(got), got)
	}
}

// TestPinHostKeysMergeSameIdentity: el host ya tiene ed25519 (casa con el
// barrido) pero le faltan ecdsa/rsa → añade solo las ausentes.
func TestPinHostKeysMergeSameIdentity(t *testing.T) {
	ed := genPubKey(t, "ed25519")
	ec := genPubKey(t, "ecdsa")
	rs := genPubKey(t, "rsa")
	scan := []khEntry{
		{hosts: []string{"192.168.1.5"}, key: ed},
		{hosts: []string{"192.168.1.5"}, key: ec},
		{hosts: []string{"192.168.1.5"}, key: rs},
	}
	existing := []khEntry{{hosts: []string{"192.168.1.5"}, key: ed}}
	got := pinHostKeysMerge("192.168.1.5", scan, existing)
	if len(got) != 2 {
		t.Fatalf("deberían añadirse 2 (ecdsa+rsa), got=%d (%v)", len(got), got)
	}
}

// TestPinHostKeysMergeChangedIdentity: el host cambió TODAS sus claves
// (reflash) → no se pinan claves nuevas: se conserva la detección de host key
// changed en lugar de aceptar en silencio la nueva identidad.
func TestPinHostKeysMergeChangedIdentity(t *testing.T) {
	oldEd := genPubKey(t, "ed25519")
	newEd := genPubKey(t, "ed25519")
	newEc := genPubKey(t, "ecdsa")
	newRs := genPubKey(t, "rsa")
	scan := []khEntry{
		{hosts: []string{"192.168.1.5"}, key: newEd},
		{hosts: []string{"192.168.1.5"}, key: newEc},
		{hosts: []string{"192.168.1.5"}, key: newRs},
	}
	existing := []khEntry{{hosts: []string{"192.168.1.5"}, key: oldEd}}
	if sameIdentity(existing, scan) {
		t.Fatalf("identidades con todas las claves distintas no deberían casar")
	}
	if shouldPinHostKeys(existing, scan) {
		t.Fatalf("reflash: no debería pinar claves nuevas")
	}
}

// TestPinHostKeysMergeNoDuplicates: el host ya tiene las 3 claves → no añade nada.
func TestPinHostKeysMergeNoDuplicates(t *testing.T) {
	ed := genPubKey(t, "ed25519")
	ec := genPubKey(t, "ecdsa")
	rs := genPubKey(t, "rsa")
	scan := []khEntry{
		{hosts: []string{"192.168.1.5"}, key: ed},
		{hosts: []string{"192.168.1.5"}, key: ec},
		{hosts: []string{"192.168.1.5"}, key: rs},
	}
	existing := []khEntry{
		{hosts: []string{"192.168.1.5"}, key: ed},
		{hosts: []string{"192.168.1.5"}, key: ec},
		{hosts: []string{"192.168.1.5"}, key: rs},
	}
	got := pinHostKeysMerge("192.168.1.5", scan, existing)
	if len(got) != 0 {
		t.Fatalf("no debería añadir duplicados, got=%d", len(got))
	}
}

// TestParseKnownHosts: parsea líneas reales (comentario ignorado, malformadas ignoradas).
func TestParseKnownHosts(t *testing.T) {
	ed := genPubKey(t, "ed25519")
	data := "# host comment\n" + lineFor(t, "192.168.1.5", ed) + "\n\nmalformada\n"
	entries, err := parseKnownHosts(data)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("se esperaba 1 entrada, got=%d", len(entries))
	}
	if entries[0].hosts[0] != "192.168.1.5" {
		t.Fatalf("host incorrecto: %v", entries[0].hosts)
	}
}
