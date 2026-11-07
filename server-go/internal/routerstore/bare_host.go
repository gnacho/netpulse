package routerstore

import (
	"database/sql"
	"log"
	"net"
	"regexp"
	"strings"
)

// bareHostRe is the contract the unit form applies to a host
// (httpapi.validateHost): an IP or a hostname, never a URL.
var bareHostRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// BareHost reduces the host typed in an integration form to the address of a
// router unit. The integration keeps the full URL because the pusher needs it,
// but the unit only accepts an IP or a hostname: the unit form rejects a scheme
// and the ping probe cannot resolve one (#1968). When the address is not
// usable, the input comes back untouched, so an odd value keeps failing where
// it already failed instead of turning into something else.
func BareHost(raw string) string {
	h := strings.TrimSpace(raw)
	if i := strings.Index(h, "://"); i >= 0 {
		h = h[i+3:]
	}
	if i := strings.LastIndex(h, "@"); i >= 0 {
		h = h[i+1:]
	}
	if i := strings.IndexAny(h, "/?#"); i >= 0 {
		h = h[:i]
	}
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	h = strings.Trim(h, "[]")
	if h == "" || !bareHostRe.MatchString(h) {
		return strings.TrimSpace(raw)
	}
	return h
}

// NormalizeExternalHosts rewrites the host of the external units created before
// #1968, which stored the integration URL and could not be edited afterwards.
// It returns how many rows changed and is safe to run on every start.
func NormalizeExternalHosts(db *sql.DB) int {
	rows, err := db.Query(`SELECT id, host FROM routers WHERE type = 'external' AND host LIKE '%://%'`)
	if err != nil {
		log.Printf("[netpulse] warning: could not read the host of external routers: %v", err)
		return 0
	}
	type pending struct {
		id   string
		host string
	}
	var fixes []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.host); err != nil {
			continue
		}
		if bare := BareHost(p.host); bare != p.host {
			p.host = bare
			fixes = append(fixes, p)
		}
	}
	rows.Close()
	for _, f := range fixes {
		if _, err := db.Exec(`UPDATE routers SET host = ? WHERE id = ?`, f.host, f.id); err != nil {
			log.Printf("[netpulse] warning: could not normalize the host of %s: %v", f.id, err)
		}
	}
	return len(fixes)
}
