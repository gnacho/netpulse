package firmware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sampleProfiles() map[string]any {
	return map[string]any{
		"profiles": map[string]any{
			"redmi_ax6": map[string]any{
				"image_prefix": "openwrt-25.12.5-qualcommax-ipq807x-redmi_ax6",
				"supported_devices": []string{"redmi,ax6"},
				"images": []map[string]any{
					{"filesystem": "squashfs", "name": "openwrt-25.12.5-qualcommax-ipq807x-redmi_ax6-squashfs-factory.ubi", "sha256": "aaaa", "type": "factory"},
					{"filesystem": "squashfs", "name": "openwrt-25.12.5-qualcommax-ipq807x-redmi_ax6-squashfs-sysupgrade.bin", "sha256": "a6729a1a5214ae61c9fdc40408951e0c84c2d1bb0b38c3496f7823fa345f22b6", "type": "sysupgrade"},
				},
			},
			"glinet_flint2": map[string]any{
				"image_prefix":   "openwrt-25.12.5-qualcommax-ipq807x-glinet_flint2",
				"supported_devices": []string{"glinet,flint2"},
				"images": []map[string]any{
					{"filesystem": "squashfs", "name": "openwrt-25.12.5-qualcommax-ipq807x-glinet_flint2-squashfs-sysupgrade.bin", "sha256": "bbbb", "type": "sysupgrade"},
				},
			},
		},
	}
}

func newServe() *httptest.Server {
	doc := sampleProfiles()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/targets/qualcommax/ipq807x/profiles.json") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(doc)
	}))
}

func TestImageResolverResolveSysupgrade(t *testing.T) {
	srv := newServe()
	defer srv.Close()
	r := NewImageResolver()
	r.BaseURL = srv.URL

	img, err := r.Resolve(context.Background(), "25.12.5", "qualcommax/ipq807x", "redmi,ax6")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := "openwrt-25.12.5-qualcommax-ipq807x-redmi_ax6-squashfs-sysupgrade.bin"
	if img.ImageName != want {
		t.Fatalf("imageName: got %q want %q", img.ImageName, want)
	}
	if img.Checksum != "a6729a1a5214ae61c9fdc40408951e0c84c2d1bb0b38c3496f7823fa345f22b6" {
		t.Fatalf("checksum: %q", img.Checksum)
	}
	if !strings.Contains(img.URL, "/releases/25.12.5/targets/qualcommax/ipq807x/"+want) {
		t.Fatalf("url: %q", img.URL)
	}
}

func TestImageResolverPrefersSysupgrade(t *testing.T) {
	srv := newServe()
	defer srv.Close()
	r := NewImageResolver()
	r.BaseURL = srv.URL
	img, err := r.Resolve(context.Background(), "25.12.5", "qualcommax/ipq807x", "redmi,ax6")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if img.ImageName == "openwrt-25.12.5-qualcommax-ipq807x-redmi_ax6-squashfs-factory.ubi" {
		t.Fatal("no debe elegir la factory si hay sysupgrade")
	}
}

func TestImageResolverNoProfile(t *testing.T) {
	srv := newServe()
	defer srv.Close()
	r := NewImageResolver()
	r.BaseURL = srv.URL
	if _, err := r.Resolve(context.Background(), "25.12.5", "qualcommax/ipq807x", "nope,missing"); err == nil {
		t.Fatal("esperaba error de perfil no encontrado")
	}
}

func TestImageResolverHTTPError(t *testing.T) {
	r := NewImageResolver()
	r.BaseURL = "http://127.0.0.1:1" // puerto no servido
	if _, err := r.Resolve(context.Background(), "25.12.5", "x/y", "a,b"); err == nil {
		t.Fatal("esperaba error de conexión/HTTP")
	}
}
