// store_test.go — consultas del agregado DNS (#2078).
package dnsactivity

import (
	"context"
	"testing"
	"time"
)

func TestTopForMACAggregatesAndOrders(t *testing.T) {
	d := testDB(t)
	st := NewStore(d)
	ctx := context.Background()
	hour := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)

	if err := st.Apply(ctx,
		[]Bucket{
			// La ingesta guarda la MAC de device_seen (mayúsculas); la API
			// consulta con la de la ruta normalizada (minúsculas).
			{MAC: "AA:BB:CC:DD:EE:01", Domain: "a.example", Hour: hour, Queries: 5, Blocked: 1, FirstSeen: hour, LastSeen: hour.Add(time.Minute)},
			{MAC: "AA:BB:CC:DD:EE:01", Domain: "b.example", Hour: hour, Queries: 9, Blocked: 0, FirstSeen: hour, LastSeen: hour},
			{MAC: "AA:BB:CC:DD:EE:02", Domain: "c.example", Hour: hour, Queries: 50, Blocked: 0, FirstSeen: hour, LastSeen: hour},
		},
		nil); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	from := hour.Add(-time.Hour)
	to := hour.Add(2 * time.Hour)
	top, err := st.TopForMAC(ctx, "aa:bb:cc:dd:ee:01", from, to, 10)
	if err != nil {
		t.Fatalf("TopForMAC: %v", err)
	}
	if len(top) != 2 {
		t.Fatalf("len = %d, want 2", len(top))
	}
	if top[0].Domain != "b.example" || top[0].Queries != 9 {
		t.Errorf("primero = %+v, want b.example con 9", top[0])
	}
	if top[1].Domain != "a.example" || top[1].Blocked != 1 {
		t.Errorf("segundo = %+v", top[1])
	}

	q, b, err := st.TotalsForMAC(ctx, "aa:bb:cc:dd:ee:01", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if q != 14 || b != 1 {
		t.Errorf("totales = %d/%d, want 14/1", q, b)
	}
}

func TestTopUsesWindow(t *testing.T) {
	d := testDB(t)
	st := NewStore(d)
	ctx := context.Background()
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

	if err := st.Apply(ctx, []Bucket{
		{MAC: "m1", Domain: "old.example", Hour: now.Add(-30 * time.Hour), Queries: 99, FirstSeen: now, LastSeen: now},
		{MAC: "m1", Domain: "new.example", Hour: now.Add(-time.Hour), Queries: 3, FirstSeen: now, LastSeen: now},
	}, nil); err != nil {
		t.Fatal(err)
	}

	top, err := st.TopAll(ctx, now.Add(-24*time.Hour), now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 1 || top[0].Domain != "new.example" {
		t.Errorf("top = %+v, la ventana de 24h no debe incluir 30h atrás", top)
	}
}

func TestNewDomains(t *testing.T) {
	d := testDB(t)
	st := NewStore(d)
	ctx := context.Background()
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

	if err := st.Apply(ctx, nil, []FirstSeen{
		{Domain: "new.example", MAC: "m1", FirstSeenMS: now.Add(-time.Minute).UnixMilli()},
		{Domain: "old.example", MAC: "m2", FirstSeenMS: now.Add(-72 * time.Hour).UnixMilli()},
	}); err != nil {
		t.Fatal(err)
	}
	// Repetir no debe duplicar ni mover la fecha.
	if err := st.Apply(ctx, nil, []FirstSeen{
		{Domain: "new.example", MAC: "m3", FirstSeenMS: now.UnixMilli()},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := st.NewDomains(ctx, now.Add(-24*time.Hour), now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Domain != "new.example" || got[0].MAC != "m1" {
		t.Errorf("new domains = %+v", got)
	}
}

func TestApplyIsAdditive(t *testing.T) {
	d := testDB(t)
	st := NewStore(d)
	ctx := context.Background()
	hour := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	b := Bucket{MAC: "m1", Domain: "a.example", Hour: hour, Queries: 2, Blocked: 1,
		FirstSeen: hour.Add(time.Minute), LastSeen: hour.Add(2 * time.Minute)}

	if err := st.Apply(ctx, []Bucket{b}, nil); err != nil {
		t.Fatal(err)
	}
	b.Queries = 3
	b.Blocked = 0
	b.FirstSeen = hour
	b.LastSeen = hour.Add(5 * time.Minute)
	if err := st.Apply(ctx, []Bucket{b}, nil); err != nil {
		t.Fatal(err)
	}

	var queries, blocked, first, last int64
	if err := d.QueryRow(`SELECT queries, blocked, first_seen, last_seen FROM dns_activity
		WHERE mac='m1' AND domain='a.example'`).Scan(&queries, &blocked, &first, &last); err != nil {
		t.Fatal(err)
	}
	if queries != 5 || blocked != 1 {
		t.Errorf("queries/blocked = %d/%d, want 5/1", queries, blocked)
	}
	if first != hour.UnixMilli() || last != hour.Add(5*time.Minute).UnixMilli() {
		t.Errorf("first/last = %d/%d", first, last)
	}
}

func TestResolveMACsCanonicalizes(t *testing.T) {
	d := testDB(t)
	if _, err := d.Exec(`INSERT INTO device_seen (mac, first_seen, last_seen, ip) VALUES
		('m1', 0, 0, ' 192.168.1.42 ')`); err != nil {
		t.Fatal(err)
	}
	macs, err := resolveMACs(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if macs["192.168.1.42"] != "m1" {
		t.Errorf("mapa = %+v", macs)
	}
}
