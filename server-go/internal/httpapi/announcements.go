// announcements.go — avisos externos para todas las instancias: el server
// consulta announcements.json del repo (raw de GitHub, como hace el updater)
// y expone el aviso activo en /api/announcement. La UI lo pinta con el
// estilo del ribbon de actualizar. Override de la fuente con
// NETPULSE_ANNOUNCEMENTS_URL (pruebas); fail-silent: sin red, sin aviso.
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

const (
	defaultAnnouncementsURL = "https://raw.githubusercontent.com/gnacho/netpulse/main/announcements.json"
	announcementsRefresh    = 6 * time.Hour
	announcementsTimeout    = 5 * time.Second
	announcementsMaxBytes   = 64 << 10
)

// Announcement es un aviso publicado en announcements.json. Title/Body van
// por idioma (es/en; el cliente cae a en). Starts/Expires ("YYYY-MM-DD",
// opcionales) delimitan la ventana de vigencia.
type Announcement struct {
	ID       string            `json:"id"`
	Urgency  string            `json:"urgency"` // "info" | "warn"
	Title    map[string]string `json:"title"`
	Body     map[string]string `json:"body,omitempty"`
	URL      string            `json:"url,omitempty"`
	URLLabel map[string]string `json:"urlLabel,omitempty"`
	Starts   string            `json:"starts,omitempty"`
	Expires  string            `json:"expires,omitempty"`
}

type announcementsFile struct {
	Announcements []Announcement `json:"announcements"`
}

type announcementCache struct {
	mu  sync.Mutex
	now func() time.Time
	url string
	cur *Announcement
}

func (c *announcementCache) set(a *Announcement) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cur = a
}

func (c *announcementCache) get() *Announcement {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cur
}

// activeAnnouncement elige el primer aviso vigente (starts <= hoy < expires).
func activeAnnouncement(list []Announcement, now time.Time) *Announcement {
	today := now.Format("2006-01-02")
	for i := range list {
		a := list[i]
		if a.ID == "" {
			continue
		}
		if a.Starts != "" && today < a.Starts {
			continue
		}
		if a.Expires != "" && today >= a.Expires {
			continue
		}
		return &a
	}
	return nil
}

func fetchAnnouncements(ctx context.Context, url string) (*Announcement, error) {
	ctx, cancel := context.WithTimeout(ctx, announcementsTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "netpulse-announcements")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("announcements: HTTP %d", res.StatusCode)
	}
	var file announcementsFile
	if err := json.NewDecoder(io.LimitReader(res.Body, announcementsMaxBytes)).Decode(&file); err != nil {
		return nil, err
	}
	return activeAnnouncement(file.Announcements, time.Now()), nil
}

// startAnnouncements lanza el bucle de refresco (daemon; vive con el server).
func (s *server) startAnnouncements() {
	url := defaultAnnouncementsURL
	if v := os.Getenv("NETPULSE_ANNOUNCEMENTS_URL"); v != "" {
		url = v
	}
	c := &announcementCache{now: time.Now, url: url}
	s.announcements = c
	refresh := func() {
		a, err := fetchAnnouncements(context.Background(), c.url)
		if err != nil {
			log.Printf("[netpulse] announcements: fetch falló: %v", err)
			return
		}
		c.set(a)
	}
	go func() {
		refresh()
		t := time.NewTicker(announcementsRefresh)
		defer t.Stop()
		for range t.C {
			refresh()
		}
	}()
}

func (s *server) registerAnnouncementRoutes(mux *http.ServeMux) {
	// Sin sesión no hay aviso (igual que el overview); en demo la sesión
	// pública de la demo lo recibe igual: el aviso va a todas las instancias.
	mux.HandleFunc("GET /api/announcement", func(w http.ResponseWriter, r *http.Request) {
		c := s.announcements
		if c == nil {
			writeJSON(w, http.StatusOK, map[string]any{"active": false})
			return
		}
		a := c.get()
		if a == nil {
			writeJSON(w, http.StatusOK, map[string]any{"active": false})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"active": true, "announcement": a})
	})
}
