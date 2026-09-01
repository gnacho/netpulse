package main

import (
	"fmt"
	"log"
	"os"

	npsnmp "github.com/gnacho/netpulse/server-go/internal/snmp"
)

func main() {
	host := os.Getenv("SNMP_HOST")
	if host == "" {
		host = "192.168.10.4"
	}
	community := os.Getenv("SNMP_COMMUNITY")
	if community == "" {
		community = "DxN3tPulse-RO-2026"
	}

	cfg := npsnmp.Config{
		Host:      host,
		Port:      161,
		Community: community,
	}
	fmt.Printf("probando SNMP contra %s\n", host)

	session, err := npsnmp.NewSession(cfg)
	if err != nil {
		log.Fatalf("NewSession: %v", err)
	}
	defer npsnmp.CloseSession(session)

	sys, err := npsnmp.PollSystem(session)
	if err != nil {
		log.Fatalf("PollSystem: %v", err)
	}
	fmt.Printf("sysName: %s\n", sys.Name)
	fmt.Printf("sysDescr: %s\n", sys.Descr)
	fmt.Printf("sysUpTime: %d s\n", sys.UpTimeSec)

	ports, err := npsnmp.PollIfTable(session)
	if err != nil {
		log.Fatalf("PollIfTable: %v", err)
	}
	fmt.Printf("puertos fisicos: %d\n", len(ports))
	for i, p := range ports {
		if i >= 5 {
			fmt.Println("...")
			break
		}
		fmt.Printf("  %s: index=%d speed=%s operUp=%v rx=%d tx=%d\n",
			p.DisplayName(), p.Index, p.SpeedString(), p.OperUp, p.RxBytes, p.TxBytes)
	}
}
