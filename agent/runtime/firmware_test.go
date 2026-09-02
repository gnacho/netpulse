// firmware_test.go — tests del upgrade de firmware OpenWrt (#453).
package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gnacho/netpulse/agent/internal/tlspin"
)

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
	imgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	tr, err := tlspin.BuildTransport(opts.Server, opts.ServerFP)
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	cmd := fmt.Sprintf(`{"upgradeId":7,"targetUrl":"%s/image.bin","checksum":"%s","keepConfig":true}`, imgServer.URL, wantHex)
	handleFirmwareUpgrade(opts, tr, cmd)

	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("sysupgrade no se ejecutó: %v", err)
	}
}
