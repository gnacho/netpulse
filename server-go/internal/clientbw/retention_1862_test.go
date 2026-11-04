package clientbw

import (
	"testing"
	"time"
)

// #1862: los buckets de 5m se guardan 90 días, no un año. Con un año la BD
// llegaba a ~1,3 GB en un contenedor de 8 GB. Este test fija la frontera: un
// bucket de hace 100 días se poda, uno de hace 89 sobrevive.
func TestPurgeKeepsNinetyDaysOfBuckets(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()

	oldBucket := now.Add(-100 * 24 * time.Hour).UnixMilli()
	recentBucket := now.Add(-89 * 24 * time.Hour).UnixMilli()
	for _, ts := range []int64{oldBucket, recentBucket} {
		_, err := s.db.Exec(`INSERT INTO client_bw_5m
			(mac, router_id, bucket_ts, n, rx_bytes, tx_bytes)
			VALUES ('aa:bb:cc:dd:ee:ff', 'rt1', ?, 1, 10, 20)`, ts)
		if err != nil {
			t.Fatal(err)
		}
	}

	oldRaw := now.Add(-8 * 24 * time.Hour).UnixMilli()
	recentRaw := now.Add(-6 * 24 * time.Hour).UnixMilli()
	for _, ts := range []int64{oldRaw, recentRaw} {
		_, err := s.db.Exec(`INSERT INTO client_bw_raw
			(mac, router_id, ts, rx_bytes, tx_bytes)
			VALUES ('aa:bb:cc:dd:ee:ff', 'rt1', ?, 10, 20)`, ts)
		if err != nil {
			t.Fatal(err)
		}
	}

	if err := s.Purge(); err != nil {
		t.Fatal(err)
	}

	var buckets int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM client_bw_5m WHERE bucket_ts = ?", oldBucket).Scan(&buckets); err != nil {
		t.Fatal(err)
	}
	if buckets != 0 {
		t.Error("un bucket de 100 días debería podarse")
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM client_bw_5m WHERE bucket_ts = ?", recentBucket).Scan(&buckets); err != nil {
		t.Fatal(err)
	}
	if buckets != 1 {
		t.Error("un bucket de 89 días debería sobrevivir")
	}

	var raws int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM client_bw_raw WHERE ts = ?", oldRaw).Scan(&raws); err != nil {
		t.Fatal(err)
	}
	if raws != 0 {
		t.Error("una muestra raw de 8 días debería podarse")
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM client_bw_raw WHERE ts = ?", recentRaw).Scan(&raws); err != nil {
		t.Fatal(err)
	}
	if raws != 1 {
		t.Error("una muestra raw de 6 días debería sobrevivir")
	}
}
