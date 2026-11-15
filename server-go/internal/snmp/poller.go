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

	// MinV3Passphrase es el mínimo que exige RFC 3414 para las passphrases
	// USM (8 caracteres). gosnmp falla con un error opaco si es más corta,
	// así que se comprueba antes para poder decírselo al usuario.
	MinV3Passphrase = 8
)

type Config struct {
	Host      string
	Port      int
	Community string
	Version   gosnmp.SnmpVersion
	// SNMPv3 (#2042): credenciales USM. Solo se usan con Version3.
	// SecurityLevel: "noAuthNoPriv", "authNoPriv" o "authPriv" ("" = authPriv).
	// AuthProto: MD5, SHA, SHA224, SHA256, SHA384, SHA512 ("" = SHA).
	// PrivProto: DES, AES, AES192, AES256, AES192C, AES256C ("" = AES).
	V3User          string
	V3SecurityLevel string
	V3AuthProto     string
	V3AuthPass      string
	V3PrivProto     string
	V3PrivPass      string
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

// ParseVersion traduce la versión guardada por router ("1", "2c", "3") al
// enum de gosnmp. Cualquier otro valor cae a v2c, que es el comportamiento
// histórico con comunidad.
func ParseVersion(v string) gosnmp.SnmpVersion {
	switch v {
	case "1":
		return gosnmp.Version1
	case "3":
		return gosnmp.Version3
	default:
		return gosnmp.Version2c
	}
}

// V3AuthProtocols son los protocolos de autenticación USM soportados por
// gosnmp. El orden es el que ve el usuario en el desplegable.
var V3AuthProtocols = []string{"MD5", "SHA", "SHA224", "SHA256", "SHA384", "SHA512"}

// V3PrivProtocols son los protocolos de cifrado USM soportados.
var V3PrivProtocols = []string{"DES", "AES", "AES192", "AES256", "AES192C", "AES256C"}

// V3SecurityLevels son los niveles de seguridad USM.
var V3SecurityLevels = []string{"noAuthNoPriv", "authNoPriv", "authPriv"}

// ValidV3AuthProtocol dice si el nombre es un protocolo de autenticación USM
// conocido (comparación exacta, mayúsculas).
func ValidV3AuthProtocol(name string) bool {
	for _, p := range V3AuthProtocols {
		if p == name {
			return true
		}
	}
	return false
}

// ValidV3PrivProtocol dice si el nombre es un protocolo de cifrado USM
// conocido.
func ValidV3PrivProtocol(name string) bool {
	for _, p := range V3PrivProtocols {
		if p == name {
			return true
		}
	}
	return false
}

// ValidV3SecurityLevel dice si el nivel es uno de los tres de USM.
func ValidV3SecurityLevel(name string) bool {
	for _, l := range V3SecurityLevels {
		if l == name {
			return true
		}
	}
	return false
}

// V3AuthProtocol mapea el nombre guardado al enum de gosnmp ("" = SHA).
func V3AuthProtocol(name string) gosnmp.SnmpV3AuthProtocol {
	switch name {
	case "MD5":
		return gosnmp.MD5
	case "SHA224":
		return gosnmp.SHA224
	case "SHA256":
		return gosnmp.SHA256
	case "SHA384":
		return gosnmp.SHA384
	case "SHA512":
		return gosnmp.SHA512
	default:
		return gosnmp.SHA
	}
}

// V3PrivProtocol mapea el nombre guardado al enum de gosnmp ("" = AES).
func V3PrivProtocol(name string) gosnmp.SnmpV3PrivProtocol {
	switch name {
	case "DES":
		return gosnmp.DES
	case "AES192":
		return gosnmp.AES192
	case "AES256":
		return gosnmp.AES256
	case "AES192C":
		return gosnmp.AES192C
	case "AES256C":
		return gosnmp.AES256C
	default:
		return gosnmp.AES
	}
}

// V3MsgFlags deriva los flags del mensaje del nivel de seguridad. A ellos se
// suma Reportable, que gosnmp añade al serializar.
func V3MsgFlags(level string) gosnmp.SnmpV3MsgFlags {
	switch level {
	case "noAuthNoPriv":
		return gosnmp.NoAuthNoPriv
	case "authNoPriv":
		return gosnmp.AuthNoPriv
	default:
		return gosnmp.AuthPriv
	}
}

// buildSession arma la sesión gosnmp sin conectar (testeable). Valida lo que
// gosnmp no explica bien: usuario vacío, nivel incoherente con las
// credenciales y passphrases cortas.
func buildSession(cfg Config) (*gosnmp.GoSNMP, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("snmp: host required")
	}
	s := &gosnmp.GoSNMP{
		Target:  cfg.Host,
		Port:    uint16(cfg.normalizedPort()),
		Version: cfg.version(),
		Timeout: DefaultTimeout,
		Retries: DefaultRetries,
	}
	if s.Version != gosnmp.Version3 {
		community := cfg.Community
		if community == "" {
			community = "public"
		}
		s.Community = community
		return s, nil
	}

	if cfg.V3User == "" {
		return nil, fmt.Errorf("snmp: SNMPv3 needs a user")
	}
	level := cfg.V3SecurityLevel
	if level == "" {
		level = "authPriv"
	}
	if !ValidV3SecurityLevel(level) {
		return nil, fmt.Errorf("snmp: unknown SNMPv3 security level %q", level)
	}
	if cfg.V3AuthProto != "" && !ValidV3AuthProtocol(cfg.V3AuthProto) {
		return nil, fmt.Errorf("snmp: unknown SNMPv3 auth protocol %q", cfg.V3AuthProto)
	}
	if cfg.V3PrivProto != "" && !ValidV3PrivProtocol(cfg.V3PrivProto) {
		return nil, fmt.Errorf("snmp: unknown SNMPv3 privacy protocol %q", cfg.V3PrivProto)
	}

	authProto := V3AuthProtocol(cfg.V3AuthProto)
	privProto := V3PrivProtocol(cfg.V3PrivProto)
	if level == "authNoPriv" || level == "authPriv" {
		if len(cfg.V3AuthPass) < MinV3Passphrase {
			return nil, fmt.Errorf("snmp: SNMPv3 auth passphrase must be at least %d characters", MinV3Passphrase)
		}
	} else {
		authProto = gosnmp.NoAuth
	}
	if level == "authPriv" {
		if len(cfg.V3PrivPass) < MinV3Passphrase {
			return nil, fmt.Errorf("snmp: SNMPv3 privacy passphrase must be at least %d characters", MinV3Passphrase)
		}
	} else {
		privProto = gosnmp.NoPriv
	}

	s.MsgFlags = V3MsgFlags(level)
	s.SecurityModel = gosnmp.UserSecurityModel
	s.SecurityParameters = &gosnmp.UsmSecurityParameters{
		UserName:                 cfg.V3User,
		AuthenticationProtocol:   authProto,
		AuthenticationPassphrase: cfg.V3AuthPass,
		PrivacyProtocol:          privProto,
		PrivacyPassphrase:        cfg.V3PrivPass,
	}
	return s, nil
}

func NewSession(cfg Config) (*gosnmp.GoSNMP, error) {
	s, err := buildSession(cfg)
	if err != nil {
		return nil, err
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
