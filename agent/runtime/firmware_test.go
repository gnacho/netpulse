// firmware_test.go — tests del upgrade de firmware OpenWrt (#453).
package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateFirmwareTarget(t *testing.T) {
	good := strings.Repeat("0", 64)
	cases := []struct {
		name     string
		url      string
		checksum string
		wantErr  bool
	}{
		{"https", "https://example.com/image.bin", good, false},
		{"http", "http://example.com/image.bin", good, true},
		{"sin host", "https://", good, true},
		{"sin checksum", "https://example.com/image.bin", "", true},
		{"checksum corto", "https://example.com/image.bin", "abc", true},
		{"checksum no hex", "https://example.com/image.bin", strings.Repeat("z", 64), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateFirmwareTarget(tc.url, tc.checksum)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateFirmwareTarget(%q, %q) err=%v wantErr=%v", tc.url, tc.checksum, err, tc.wantErr)
			}
		})
	}
}

func TestDownloadImageRejectsEmptyChecksum(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "image.bin")
	if err := downloadImage(context.Background(), nil, "https://example.com/image.bin", dest, "", nil); err == nil {
		t.Fatal("esperaba error por checksum vacio")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("no deberia quedar el fichero: %v", err)
	}
}

func TestDownloadImageRejectsOversize(t *testing.T) {
	orig := maxFirmwareImageBytes
	maxFirmwareImageBytes = 1024
	defer func() { maxFirmwareImageBytes = orig }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 2048))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "image.bin")
	err := downloadImage(context.Background(), srv.Client(), srv.URL, dest, strings.Repeat("0", 64), nil)
	if err == nil || !strings.Contains(err.Error(), "mayor") {
		t.Fatalf("esperaba error de tamano, got %v", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("no deberia quedar el fichero: %v", statErr)
	}
}

func TestVerifyImageSha256(t *testing.T) {
	data := []byte("openwrt-image-stub")
	want := sha256.Sum256(data)
	wantHex := hex.EncodeToString(want[:])
	path := filepath.Join(t.TempDir(), "img.bin")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyImageSha256(path, wantHex); err != nil {
		t.Fatalf("verificación correcta: %v", err)
	}
	if err := verifyImageSha256(path, "bad"); err == nil {
		t.Fatal("esperaba error con checksum malo")
	}
}

func TestFirmwarePendingFile(t *testing.T) {
	orig := firmwarePendingFile
	defer func() { firmwarePendingFile = orig }()

	tmp := t.TempDir()
	firmwarePendingFile = filepath.Join(tmp, "pending.json")

	p, err := readPendingFirmware()
	if err != nil || p != nil {
		t.Fatalf("sin pending: %v %v", p, err)
	}

	if err := writePendingFirmware(pendingFirmware{UpgradeID: 42}); err != nil {
		t.Fatalf("write: %v", err)
	}
	p, err = readPendingFirmware()
	if err != nil || p == nil || p.UpgradeID != 42 {
		t.Fatalf("read: %v %v", p, err)
	}
	if err := clearPendingFirmware(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	p, err = readPendingFirmware()
	if err != nil || p != nil {
		t.Fatalf("after clear: %v %v", p, err)
	}
}

func TestHandleFirmwareUpgradeInvokesSysupgrade(t *testing.T) {
	image := []byte("openwrt-firmware-image")
	wantHash := sha256.Sum256(image)
	wantHex := hex.EncodeToString(wantHash[:])
	imgServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(image)
	}))
	defer imgServer.Close()

	tmp := t.TempDir()
	marker := filepath.Join(tmp, "sysupgrade-ran")
	fakeSysupgrade := filepath.Join(tmp, "fake-sysupgrade")
	script := fmt.Sprintf("#!/bin/sh\ntouch %s\nexit 0", marker)
	if err := os.WriteFile(fakeSysupgrade, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	origBin := sysupgradeBin
	sysupgradeBin = fakeSysupgrade
	defer func() { sysupgradeBin = origBin }()

	origPending := firmwarePendingFile
	firmwarePendingFile = filepath.Join(tmp, "pending.json")
	defer func() { firmwarePendingFile = origPending }()

	opts := Options{
		Server: imgServer.URL,
		Slug:   "rt-fw",
		Token:  "token",
	}
	tr := imgServer.Client().Transport
	cmd := fmt.Sprintf(`{"upgradeId":7,"targetUrl":"%s/image.bin","checksum":"%s","keepConfig":true}`, imgServer.URL, wantHex)
	handleFirmwareUpgrade(opts, tr, cmd)

	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("sysupgrade no se ejecutó: %v", err)
	}
}
