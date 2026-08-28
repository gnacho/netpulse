package snmp

import (
	"fmt"
	"time"

	"github.com/gosnmp/gosnmp"
)

const (
	DefaultPort    = 161
	DefaultTimeout = 5 * time.Second
	DefaultRetries = 1
)

type Config struct {
	Host      string
	Port      int
	Community string
	Version   gosnmp.SnmpVersion
}

func (c Config) normalizedPort() int {
	if c.Port > 0 {
		return c.Port
	}
	return DefaultPort
}

func (c Config) version() gosnmp.SnmpVersion {
	if c.Version != 0 {
		return c.Version
	}
	return gosnmp.Version2c
}

func NewSession(cfg Config) (*gosnmp.GoSNMP, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("snmp: host required")
	}
	community := cfg.Community
	if community == "" {
		community = "public"
	}
	s := &gosnmp.GoSNMP{
		Target:    cfg.Host,
		Port:      uint16(cfg.normalizedPort()),
		Community: community,
		Version:   cfg.version(),
		Timeout:   DefaultTimeout,
		Retries:   DefaultRetries,
	}
	if err := s.Connect(); err != nil {
		return nil, fmt.Errorf("snmp connect: %w", err)
	}
	return s, nil
}

func CloseSession(s *gosnmp.GoSNMP) {
	if s == nil {
		return
	}
	_ = s.Conn.Close()
}
