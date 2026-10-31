package rintegration

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func buildTarGz(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range entries {
		hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// startReleaseServer serves a fixed release index plus the tarballs and their
// sha256 companions, with absolute asset URLs derived from the request host.
func startReleaseServer(t *testing.T, tarballs map[string][]byte, releases []map[string]any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/gnacho/netpulse-integrations/releases", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		rel := make([]map[string]any, len(releases))
		for i, rel0 := range releases {
			rel[i] = map[string]any{
				"tag_name":   rel0["tag_name"],
				"prerelease": rel0["prerelease"],
			}
			if assets, ok := rel0["assets"].([]map[string]any); ok {
				abs := make([]map[string]any, len(assets))
				for j, a := range assets {
					abs[j] = map[string]any{
						"name":                 a["name"],
						"browser_download_url": base + a["browser_download_url"].(string),
					}
				}
				rel[i]["assets"] = abs
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rel)
	})
	for name, data := range tarballs {
		data := data
		mux.HandleFunc("/a/"+name, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(data)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestResolveLatestSkipsPrereleaseAndWrongPrefix(t *testing.T) {
	v200 := buildTarGz(t, map[string]string{"routeros-pusher": "v2"})
	releases := []map[string]any{
		{
			"tag_name": "routeros/v3.0.0-rc1", "prerelease": true,
			"assets": []map[string]any{},
		},
		{
			"tag_name": "routeros/v2.0.0", "prerelease": false,
			"assets": []map[string]any{
				{"name": "routeros_2.0.0_linux_amd64.tar.gz", "browser_download_url": "/a/routeros_2.0.0_linux_amd64.tar.gz"},
				{"name": "routeros_2.0.0_linux_amd64.tar.gz.sha256", "browser_download_url": "/a/routeros_2.0.0_linux_amd64.tar.gz.sha256"},
			},
		},
		{
			"tag_name": "routeros/v1.0.0", "prerelease": false,
			"assets": []map[string]any{},
		},
	}
	srv := startReleaseServer(t, map[string][]byte{
		"routeros_2.0.0_linux_amd64.tar.gz":        v200,
		"routeros_2.0.0_linux_amd64.tar.gz.sha256": []byte(sha256Hex(v200)),
	}, releases)

	f := NewFetcher()
	f.APIBase = srv.URL
	v := seedVendor(t, "routeros")
	res, err := f.ResolveLatest(v, "amd64")
	if err != nil {
		t.Fatalf("ResolveLatest: %v", err)
	}
	if res.Version != "2.0.0" {
		t.Fatalf("version = %q, want 2.0.0", res.Version)
	}
	if res.AssetName != "routeros_2.0.0_linux_amd64.tar.gz" {
		t.Fatalf("asset = %q", res.AssetName)
	}
	if res.SHA256 != sha256Hex(v200) {
		t.Fatalf("sha256 mismatch")
	}
}

func TestResolveLatestAsuswrt(t *testing.T) {
	asus := buildTarGz(t, map[string]string{"asuswrt-pusher": "a"})
	releases := []map[string]any{
		{
			"tag_name": "asuswrt/v1.0.0", "prerelease": false,
			"assets": []map[string]any{
				{"name": "asuswrt_1.0.0_linux_arm64.tar.gz", "browser_download_url": "/a/asuswrt_1.0.0_linux_arm64.tar.gz"},
				{"name": "asuswrt_1.0.0_linux_arm64.tar.gz.sha256", "browser_download_url": "/a/asuswrt_1.0.0_linux_arm64.tar.gz.sha256"},
			},
		},
	}
	srv := startReleaseServer(t, map[string][]byte{
		"asuswrt_1.0.0_linux_arm64.tar.gz":        asus,
		"asuswrt_1.0.0_linux_arm64.tar.gz.sha256": []byte(sha256Hex(asus)),
	}, releases)

	f := NewFetcher()
	f.APIBase = srv.URL
	v := seedVendor(t, "asuswrt")
	res, err := f.ResolveLatest(v, "arm64")
	if err != nil {
		t.Fatalf("ResolveLatest: %v", err)
	}
	if res.Version != "1.0.0" || res.AssetName != "asuswrt_1.0.0_linux_arm64.tar.gz" {
		t.Fatalf("unexpected resolve: %+v", res)
	}
}

func TestDownloadVerified(t *testing.T) {
	content := "#!/bin/sh\necho ok\n"
	tgz := buildTarGz(t, map[string]string{"routeros-pusher": content})
	res := Resolved{
		Version: "2.0.0", AssetName: "routeros_2.0.0_linux_amd64.tar.gz",
		URL: "/a/x.tar.gz", SHA256: sha256Hex(tgz),
	}
	srv := startReleaseServer(t, map[string][]byte{"x.tar.gz": tgz}, nil)
	res.URL = srv.URL + res.URL

	f := NewFetcher()
	dest := t.TempDir()
	bin, err := f.Download(res, "routeros-pusher", dest)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("read binary: %v", err)
	}
	if string(got) != content {
		t.Fatalf("binary content = %q", got)
	}
}

func TestDownloadBadSHA256(t *testing.T) {
	tgz := buildTarGz(t, map[string]string{"routeros-pusher": "x"})
	res := Resolved{URL: "/a/x.tar.gz", SHA256: strings.Repeat("deadbeef", 8)}
	srv := startReleaseServer(t, map[string][]byte{"x.tar.gz": tgz}, nil)
	res.URL = srv.URL + res.URL

	f := NewFetcher()
	if _, err := f.Download(res, "routeros-pusher", t.TempDir()); err == nil {
		t.Fatal("expected sha256 mismatch error")
	}
}

func TestExtractTarGzRejectsUnsafePaths(t *testing.T) {
	for _, name := range []string{"../evil", "/abs", "a/../../evil"} {
		tgz := buildTarGz(t, map[string]string{name: "bad"})
		if _, err := extractTarGz(bytes.NewReader(tgz), t.TempDir()); err == nil {
			t.Fatalf("extractTarGz accepted unsafe path %q", name)
		}
	}
}

func TestExtractTarGzNestedBinary(t *testing.T) {
	tgz := buildTarGz(t, map[string]string{"bin/routeros-pusher": "x", "README": "r"})
	dest := t.TempDir()
	got, err := extractTarGz(bytes.NewReader(tgz), dest)
	if err != nil {
		t.Fatalf("extractTarGz: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 files, got %d", len(got))
	}
	if _, err := os.Stat(filepath.Join(dest, "bin", "routeros-pusher")); err != nil {
		t.Fatalf("binary not extracted: %v", err)
	}
}

func TestParseSHA256(t *testing.T) {
	digest := sha256Hex([]byte("x"))
	if got, err := parseSHA256(digest); err != nil || got != digest {
		t.Fatalf("bare digest: %q, %v", got, err)
	}
	if got, err := parseSHA256(digest + "  some-file.tar.gz\n"); err != nil || got != digest {
		t.Fatalf("sha256sum form: %q, %v", got, err)
	}
	if _, err := parseSHA256("not-a-digest"); err == nil {
		t.Fatal("expected error for invalid digest")
	}
}
