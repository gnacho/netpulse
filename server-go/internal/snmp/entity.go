// entity.go — ENTITY-MIB (RFC 6933, before RFC 4133) best-effort reader.
//
// Some SNMP switches leave sysDescr as the only identity and publish no
// firmware anywhere else, yet they carry the firmware and the short model in
// entPhysicalTable. The chassis(3) row is the unit itself: a stackable switch
// can put a stack(11) row first, so choosing by row index is wrong.
//
// Firmware uses entPhysicalSoftwareRev first and entPhysicalFirmwareRev as
// the fallback: both columns are standard but vendors populate different
// ones. A missing column stays empty, the same null contract as #441/#1036.
package snmp

import (
	"sort"
	"strconv"
	"strings"

	"github.com/gosnmp/gosnmp"
)

const (
	OidEntPhysicalTable = ".1.3.6.1.2.1.47.1.1.1.1"

	oidEntPhysicalClass       = OidEntPhysicalTable + ".5"
	oidEntPhysicalHardwareRev = OidEntPhysicalTable + ".8"
	oidEntPhysicalFirmwareRev = OidEntPhysicalTable + ".9"
	oidEntPhysicalSoftwareRev = OidEntPhysicalTable + ".10"
	oidEntPhysicalSerialNum   = OidEntPhysicalTable + ".11"
	oidEntPhysicalModelName   = OidEntPhysicalTable + ".13"
)

const entPhysicalClassChassis = 3

// EntityInfo is the identity the ENTITY-MIB publishes for the unit.
type EntityInfo struct {
	Firmware   string
	ModelShort string
}

type entityRow struct {
	index       int
	class       int
	firmwareRev string
	softwareRev string
	modelName   string
	serialNum   string
}

// PollEntityInfo walks entPhysicalTable once and returns the chassis row.
// A device without the table yields an empty EntityInfo and no error, like
// the other optional probes.
func PollEntityInfo(s *gosnmp.GoSNMP) (EntityInfo, error) {
	pdus, err := walkSafe(s, OidEntPhysicalTable)
	if err != nil {
		return EntityInfo{}, nil
	}
	return ParseEntityTable(pdus), nil
}

// ParseEntityTable selects the chassis row and falls back to the first row
// that publishes anything, in index order.
func ParseEntityTable(pdus []gosnmp.SnmpPDU) EntityInfo {
	rows := map[int]*entityRow{}
	for _, v := range pdus {
		col, idx := entityColumn(v.Name)
		if col == "" || idx == 0 {
			continue
		}
		row := rows[idx]
		if row == nil {
			row = &entityRow{index: idx}
			rows[idx] = row
		}
		switch col {
		case oidEntPhysicalClass:
			row.class = int(uint64Val(v))
		case oidEntPhysicalFirmwareRev:
			row.firmwareRev = strings.TrimSpace(stringVal(v))
		case oidEntPhysicalSoftwareRev:
			row.softwareRev = strings.TrimSpace(stringVal(v))
		case oidEntPhysicalModelName:
			row.modelName = strings.TrimSpace(stringVal(v))
		case oidEntPhysicalSerialNum:
			row.serialNum = strings.TrimSpace(stringVal(v))
		}
	}
	if len(rows) == 0 {
		return EntityInfo{}
	}
	ordered := make([]*entityRow, 0, len(rows))
	for _, row := range rows {
		ordered = append(ordered, row)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].index < ordered[j].index })

	chosen := ordered[0]
	for _, row := range ordered {
		if row.class == entPhysicalClassChassis && row.hasIdentity() {
			chosen = row
			break
		}
		if !chosen.hasIdentity() && row.hasIdentity() {
			chosen = row
		}
	}
	return EntityInfo{Firmware: chosen.firmware(), ModelShort: chosen.modelName}
}

func (r *entityRow) hasIdentity() bool {
	return r.firmware() != "" || r.modelName != "" || r.serialNum != ""
}

func (r *entityRow) firmware() string {
	if r.softwareRev != "" {
		return r.softwareRev
	}
	return r.firmwareRev
}

// entityColumn splits an entPhysicalTable OID into its column and row index.
func entityColumn(name string) (string, int) {
	if !strings.HasPrefix(name, OidEntPhysicalTable+".") {
		return "", 0
	}
	rest := strings.TrimPrefix(name, OidEntPhysicalTable+".")
	col, idx, ok := strings.Cut(rest, ".")
	if !ok || strings.Contains(idx, ".") {
		return "", 0
	}
	n, err := strconv.Atoi(idx)
	if err != nil || n <= 0 {
		return "", 0
	}
	return OidEntPhysicalTable + "." + col, n
}
