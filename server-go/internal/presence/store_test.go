package presence

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func TestPeopleCRUD(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	s := NewStore(d)
	if err := s.AddPerson(Person{Name: "Nacho", MACs: []string{"aa:bb:cc:dd:ee:ff"}, Color: "#f59e0b"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	people := s.ListPeople()
	if len(people) != 1 || people[0].Name != "Nacho" {
		t.Fatalf("list: %+v", people)
	}

	if s.PersonForMAC("aa:bb:cc:dd:ee:ff") == "" {
		t.Fatal("MAC not indexed")
	}

	s.DeletePerson(people[0].ID)
	if len(s.ListPeople()) != 0 {
		t.Fatal("delete failed")
	}
}

func TestPersistence(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	s := NewStore(d)
	_ = s.AddPerson(Person{Name: "Ana", MACs: []string{"11:22:33:44:55:66"}})

	s2 := NewStore(d)
	people := s2.ListPeople()
	if len(people) != 1 || people[0].Name != "Ana" {
		t.Fatalf("persistence: %+v", people)
	}
}

func TestCurrentStatus(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	if _, err := d.Exec("INSERT INTO device_events (ts_ms, mac, state) VALUES (1000, 'aa:bb:cc:dd:ee:ff', 'online')"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	s := NewStore(d)
	_ = s.AddPerson(Person{ID: "p1", Name: "Test", MACs: []string{"aa:bb:cc:dd:ee:ff"}})

	statuses := s.CurrentStatus()
	if len(statuses) != 1 || !statuses[0].Home {
		t.Fatalf("status: %+v", statuses)
	}
}
