// announcements_test.go — avisos externos: selección del aviso vigente
// (starts/expires) y fetch con servidor de prueba (fail-silent fuera).
package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestActiveAnnouncementWindow(t *testing.T) {
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	list := []Announcement{
		{ID: "futuro", Starts: "2026-10-01"},
		{ID: "caducado", Expires: "2026-09-01"},
		{ID: "sin-ventana", Title: map[string]string{"en": "always"}},
		{ID: "vigente-con-fin", Starts: "2026-09-01", Expires: "2026-11-01"},
	}
	got := activeAnnouncement(list, now)
	if got == nil || got.ID != "sin-ventana" {
		t.Fatalf("debe elegir el primero vigente sin ventana: %+v", got)
	}
	solo := []Announcement{{ID: "vigente-con-fin", Starts: "2026-09-01", Expires: "2026-11-01"}}
	if got := activeAnnouncement(solo, now); got == nil || got.ID != "vigente-con-fin" {
		t.Fatalf("ventana válida debe activarse: %+v", got)
	}
	if got := activeAnnouncement(nil, now); got != nil {
		t.Fatalf("lista vacía: sin aviso, got %+v", got)
	}
}

func TestFetchAnnouncementsPicksActive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"announcements":[{"id":"caducado","expires":"2020-01-01"},{"id":"vivo","title":{"es":"hola","en":"hello"}}]}`))
	}))
	defer srv.Close()
	a, err := fetchAnnouncements(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if a == nil || a.ID != "vivo" || a.Title["en"] != "hello" {
		t.Fatalf("aviso activo incorrecto: %+v", a)
	}
}

func TestFetchAnnouncementsFailsSilent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, err := fetchAnnouncements(context.Background(), srv.URL); err == nil {
		t.Fatal("un 500 debe devolver error (el caller lo traga con log)")
	}
}
