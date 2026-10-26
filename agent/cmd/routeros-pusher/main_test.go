package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

func TestParseRouterOSUptime(t *testing.T) {
	cases := map[string]float64{
		"5s":         5,
		"4m5s":       245,
		"3h4m5s":     11045,
		"2d3h4m5s":   183845,
		"1w2d3h4m5s": 788645,
	}
	for input, want := range cases {
		got, err := parseRouterOSUptime(input)
		if err != nil {
			t.Fatalf("parseRouterOSUptime(%q): %v", input, err)
		}
		if got != want {
			t.Errorf("parseRouterOSUptime(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestParseRouterOSUptimeInvalid(t *testing.T) {
	if _, err := parseRouterOSUptime("not-a-duration"); err == nil {
		t.Fatal("expected error for unparseable uptime")
	}
}

func TestBuildSystemData(t *testing.T) {
	res := routerOSResource{
		CPULoad:      "3",
		FreeMemory:   "104857600",
		TotalMemory:  "134217728",
		Uptime:       "1d02h03m04s",
		Version:      "7.15",
		BoardName:    "RB4011",
		Architecture: "arm",
	}
	id := routerOSIdentity{Name: "MikroTik-Core"}

	sys, err := buildSystemData(res, id)
	if err != nil {
		t.Fatalf("buildSystemData: %v", err)
	}
	if sys.CPU == nil || *sys.CPU != 3 {
		t.Errorf("CPU = %v, want 3", sys.CPU)
	}
	if sys.SysInfo.Memory.Total != 134217728 || sys.SysInfo.Memory.Free != 104857600 {
		t.Errorf("memory = %+v", sys.SysInfo.Memory)
	}
	if sys.Board.Hostname != "MikroTik-Core" || sys.Board.Release.Version != "7.15" {
		t.Errorf("board = %+v", sys.Board)
	}
}

func TestBuildSystemDataBadCPU(t *testing.T) {
	res := routerOSResource{CPULoad: "n/a", Uptime: "1s"}
	if _, err := buildSystemData(res, routerOSIdentity{}); err == nil {
		t.Fatal("expected error for bad cpu-load")
	}
}

func TestPushPayloadSignsBody(t *testing.T) {
	const token = "routeros-test-token"
	var gotAuth, gotSignature string
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ingest/agent" {
			t.Errorf("path = %q, want /api/ingest/agent", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		gotSignature = r.Header.Get("X-Agent-Signature")
		gotBody, _ = io.ReadAll(r.Body)

		mac := hmac.New(sha256.New, []byte(token))
		mac.Write(gotBody)
		want := hex.EncodeToString(mac.Sum(nil))
		if gotAuth != "Bearer "+token || !hmac.Equal([]byte(gotSignature), []byte(want)) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	payload := probe.Payload{
		Router:   "routeros-test",
		Kind:     "external",
		Ts:       1,
		Interval: 30,
	}
	if err := pushPayload(context.Background(), srv.Client(), srv.URL, token, payload); err != nil {
		t.Fatalf("pushPayload: %v", err)
	}
	if gotAuth != "Bearer "+token {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotSignature == "" {
		t.Fatal("X-Agent-Signature is empty")
	}
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write(gotBody)
	if want := hex.EncodeToString(mac.Sum(nil)); gotSignature != want {
		t.Errorf("X-Agent-Signature = %q, want %q", gotSignature, want)
	}
}
