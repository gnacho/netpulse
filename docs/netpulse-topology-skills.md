# NetPulse: Skills y Recursos para Arreglar la Topología de Red

> Problema: El colector asigna `routerId` por quién reporta la MAC en su ARP, no por topología física real. Dispositivos cableados tras `switch16` se ven en el ARP de `rt2` (porque `rt2` está bridged al switch) y el colector les pone `routerId=redmi-ax6`. Luego `hubOf` en el servidor no los puede corregir porque no tienen `AttachTo` ni `Port`.

---

## Índice

1. [Patrón de Arquitectura: Reconciliación Multi-Fuente](#1-patrón-de-arquitectura-reconciliación-multi-fuente)
2. [Librerías Go Recomendadas](#2-librerías-go-recomendadas)
   - [A) ubus (OpenWrt)](#a-ubus-openwrt)
   - [B) Bridge FDB vía netlink](#b-bridge-fdb-vía-netlink)
   - [C) LLDP](#c-lldp)
   - [D) SNMP (switches no-OpenWrt)](#d-snmp-switches-no-openwrt)
   - [E) SQLite WAL](#e-sqlite-wal)
3. [Análisis de Opciones A / B / C](#3-análisis-de-opciones-a--b--c)
4. [Estructura de Código Sugerida](#4-estructura-de-código-sugerida)
5. [Fuentes y Referencias](#5-fuentes-y-referencias)

---

## 1. Patrón de Arquitectura: Reconciliación Multi-Fuente

Tu problema es clásico de **reconciliación de múltiples fuentes de datos con granularidad diferente**. El colector reporta "vista L3" (ARP) y necesitas inferir "vista L2" (FDB + LLDP). Los NMS maduros (NetXMS, WhatsUp Gold) resuelven esto con un **pipeline de reconciliación** en el servidor, no confiando ciegamente en lo que reporta cada agente.

### Diagrama del patrón

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  Source 1   │     │  Source 2   │     │  Source 3   │
│  ARP table  │     │  Bridge FDB │     │   LLDP      │
│  (router)   │     │  (switch)   │     │  (switch)   │
└──────┬──────┘     └──────┬──────┘     └──────┬──────┘
       │                   │                   │
       └───────────────────┼───────────────────┘
                           ▼
              ┌─────────────────────┐
              │  Reconciliation     │
              │  Engine (server)    │
              │  - MAC → port (FDB) │
              │  - Port → neighbor  │
              │    (LLDP)           │
              │  - Confidence score   │
              └─────────────────────┘
```

### Cómo lo hacen los NMS maduros

**NetXMS** implementa exactamente esto:

> *"From FDB table we take ports where only one MAC address is present — this means that something is directly connected. If this device is present in NetXMS and its MAC address is known, we have a peer."*

> *"LLDP: if we have another switch connected, that switch is sending LLDP packets... we read this table and we know that there's a device with some LLDP ID connected to port X."*

**WhatsUp Gold** usa un patrón similar:

> *"WhatsUp Gold uses layer 2 and layer 3 discovery to identify and map devices on your network. Layer 2 discovery uses the Address Resolution Protocol (ARP) cache... Layer 3 discovery uses the Simple Network Management Protocol (SNMP) to query devices for information."*

**Aplicación a tu caso:**
- `switch16` debe reportar su propia FDB (no depender de que `rt2` la vea en ARP)
- El servidor debe cruzar MAC → port → neighbor (LLDP) para determinar el attachment point real
- Los dispositivos sin `AttachTo` ni `Port` pero presentes en FDB de un switch conocido → se les asigna `routerId = switch16`

---

## 2. Librerías Go Recomendadas

### A) ubus (OpenWrt)

Tu colector habla con OpenWrt vía ubus. Opciones mejores que parsear raw:

| Librería | URL | Características |
|----------|-----|-----------------|
| `goubus` | `github.com/honeybbq/goubus` | Type-safe, dual transport (HTTP JSON-RPC + Unix socket), manager pattern (`client.System()`, `client.Network()`). Soporta exec de comandos vía `client.File().Exec()`. |
| `go-ubus-rpc` | `github.com/daimonaslabs/go-ubus-rpc` | CLI + library, typed results, maneja todo el marshaling de ubus responses. |

**Skill:** Si tu colector actual parsea output de `ssh` o `ubus` raw, migrar a una de estas librerías te da type safety y reduce bugs de parsing.

### B) Bridge FDB vía netlink

En vez de hacer `ssh router "bridge fdb show"` y parsear texto, puedes usar **`github.com/vishvananda/netlink`** directamente desde Go.

```go
import "github.com/vishvananda/netlink"

// Listar FDB (Forwarding Database)
neighs, err := netlink.NeighList(0, netlink.FAMILY_ALL)
// Filtrar solo entries de tipo bridge fdb
for _, n := range neighs {
    if n.Family == netlink.FAMILY_BRIDGE {
        // n.IP, n.HardwareAddr, n.LinkIndex, n.State
    }
}
```

**Funciones clave:**
- `NeighList()` — Equivalente a `ip neighbor show`
- `NeighSubscribe()` — Updates en tiempo real vía netlink socket
- `LinkList()` — Interfaces disponibles

**Esto es clave para tu opción A (fix en servidor):** si `switch16` corre Linux (OpenWrt o similar), puedes leer su FDB vía netlink y cruzar MAC→port sin depender de lo que reporte el agente del router.

### C) LLDP

| Librería | URL | Características |
|----------|-----|-----------------|
| `gotopo/lldp` | `github.com/ksang/gotopo/lldp` | Estructuras de datos y colección de endpoints LLDP. |
| `lldp2map` | `github.com/buraglio/lldp2map` | Herramienta Go que hace walk recursivo de tablas LLDP vía SNMP y genera diagramas. Estudia cómo resuelve neighbor discovery. |

**Nota:** Para LLDP nativo en OpenWrt, necesitas `lldpd` instalado. El daemon expone datos vía:
- Unix socket (`/var/run/lldpd.socket`)
- SNMP (si está habilitado)
- CLI (`lldpcli show neighbors`)

### D) SNMP (switches no-OpenWrt)

| Librería | URL | Características |
|----------|-----|-----------------|
| `gosnmp` | `github.com/gosnmp/gosnmp` | Librería SNMP estándar en Go. Soporta v1/v2c/v3, Walk, BulkWalk, Traps. |

**OIDs útiles para topología:**
- `1.3.6.1.2.1.17.4.3` — `dot1dTpFdbTable` (FDB/bridge MAC table)
- `1.0.8802.1.1.2.1.4.1` — `lldpRemTable` (LLDP neighbors)
- `1.3.6.1.2.1.2.2.1.2` — `ifDescr` (interface descriptions)

Esto te abre la puerta a soportar switches que no sean OpenWrt (managed switches con SNMP LLDP).

### E) SQLite WAL

Usas `modernc.org/sqlite` (bien, sin CGO). Asegúrate de tener los pragmas correctos para el patrón "un writer, muchos readers" que necesitas con SSE + collector concurrente:

```go
dsn := fmt.Sprintf(
    "%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)",
    path,
)
db.SetMaxOpenConns(1)  // CRÍTICO para SQLite
db.SetMaxIdleConns(1)
```

**Si necesitas reads concurrentes mientras escribes**, la recomendación es **dos instancias `*sql.DB`**:
- Una para writes (`MaxOpenConns=1`)
- Otra para reads (`MaxOpenConns` ilimitado, WAL permite concurrent reads)

---

## 3. Análisis de Opciones A / B / C

### Hechos verificados (de tu análisis)

1. `/api/overview` devuelve 69 devices online, 51 wifi + 18 wired
2. 46 devices en el ring del gateway (cap=13, overflow +33) — la mayoría mal asignados
3. 12 wired bajo `rt2` que físicamente están tras `switch16` → el colector los asignó a `rt2` porque `rt2` los ve en su ARP
4. `switch16` (5º router, `roleBadge=SW`) queda fuera de `routerNodes` (hardcodeado a máx 4: gw + 3 APs)
5. Los 3 CTs bajo `switch16` (citadel-01/02, PBS) no aparecen en ningún ring
6. Los distnodes (inferred en lan3, hypervisor en switch16) se crean pero el hipervisor no genera enlaces porque su router (`switch16`) no está en `routerNodes`

### Opción A: Fix en el servidor (Recomendado como parche inmediato)

**Qué hace:** Implementa un **Topology Reconciler** en el servidor que:

1. **Ingesta múltiples fuentes** por router/switch:
   - `arp_table` (de cada agente)
   - `bridge_fdb` (de cada dispositivo que lo soporte, incluido `switch16`)
   - `lldp_neighbors` (de cada dispositivo)

2. **Asigna `routerId` por evidencia, no por reporte:**
   - Si un dispositivo cableado aparece en el FDB de `switch16` en `port X`, y `switch16` es un nodo conocido → `routerId = switch16`.
   - Si aparece en FDB de `rt2` pero también en FDB de `switch16` → la fuente "más específica" (`switch16`, que es L2 puro) gana.
   - Usa un **confidence score**: FDB directo > ARP indirecto.

3. **Expande `routerNodes` dinámicamente:**
   - En vez de hardcodear 4 nodos, detecta switches vía LLDP o FDB y los añade como `roleBadge=SW` con su propio ring.

4. **Genera `attachTo` + `port` inferidos:**
   - Del FDB: MAC → port → si el port tiene un LLDP neighbor conocido, `attachTo = neighbor`.
   - Si no hay LLDP, `attachTo = switch16` (el switch es el attachment point).

**Pros:**
- No toca firmware de routers
- Funciona con datos actuales del colector
- Contained, reversible

**Cons:**
- Lógica más compleja en servidor
- Sigue dependiendo de ARP "sucio" del colector

### Opción B: Fix en el colector (Rediseño del contrato)

**Qué hace:** Cambia el contrato agente→server para que cada agente reporte:

- **WiFi clients:** solo los que tiene asociados (`iwinfo`/`ubus`)
- **Wired clients:** solo los que aparecen en su **FDB local** con `master br0` y no son `self`/`permanent` de otros puertos
- **ARP table:** como fuente secundaria, etiquetada como `source:arp`, no como `source:direct`

**Pros:**
- Datos más limpios desde la fuente
- Menos trabajo de reconciliación en servidor
- Cada agente solo reporta lo que "ve" realmente

**Cons:**
- Requiere desplegar nuevo firmware en todos los routers
- Cambia el contrato agente→server (breaking change)
- No resuelve el caso de `switch16` si no tiene agente propio

### Opción C: Ambos (Recomendación final)

**Voto por C, con prioridad en A como parche inmediato.**

- **A** resuelve tu problema *ahora* sin tocar firmware de routers.
- **B** reduce la carga de reconciliación y hace el sistema más robusto a largo plazo.
- **A+B juntos** = el servidor tiene un reconciliador defensivo (por si un agente miente o no sabe) y los agentes reportan datos más limpios.

**Roadmap sugerido:**

```
Fase 1 (ahora):   Implementar Reconciler en servidor (Opción A)
                  → switch16 aparece en topology, CTs visibles
                  → 46 devices del gateway se redistribuyen

Fase 2 (próximo): Rediseñar contrato del colector (Opción B)
                  → Cada agente reporta solo dispositivos "propios"
                  → Reducir carga de reconciliación

Fase 3 (futuro):  Soporte SNMP para switches no-OpenWrt
                  → NetPulse soporta cualquier switch managed
```

---

## 4. Estructura de Código Sugerida

### 4.1 Reconciler

```go
// pkg/topology/reconciler.go
package topology

import (
    "database/sql"
    "fmt"
    "sort"
)

// EvidenceSource indica de dónde viene la evidencia
type EvidenceSource string

const (
    SourceARP        EvidenceSource = "arp"
    SourceFDB        EvidenceSource = "fdb"
    SourceLLDP       EvidenceSource = "lldp"
    SourceWiFiAssoc  EvidenceSource = "wifi_assoc"
)

// DeviceEvidence es una observación de un dispositivo en la red
type DeviceEvidence struct {
    MAC        string
    IP         string
    Source     EvidenceSource
    RouterID   string   // quien reportó
    Port       string   // para FDB/LLDP
    NeighborID string   // para LLDP
    Confidence int      // 1-10
    Timestamp  int64
}

// Reconciler engine de topología
type Reconciler struct {
    db *sql.DB
}

func NewReconciler(db *sql.DB) *Reconciler {
    return &Reconciler{db: db}
}

// Reconcile toma evidencia cruda y produce dispositivos con routerId correcto
func (r *Reconciler) Reconcile(evidence []DeviceEvidence) (map[string]*Device, error) {
    byMAC := make(map[string][]DeviceEvidence)

    // 1. Agrupar por MAC
    for _, e := range evidence {
        byMAC[e.MAC] = append(byMAC[e.MAC], e)
    }

    devices := make(map[string]*Device)

    for mac, evs := range byMAC {
        // 2. Ordenar por confidence (mayor primero)
        sort.Slice(evs, func(i, j int) bool {
            return evs[i].Confidence > evs[j].Confidence
        })

        best := evs[0]

        device := &Device{
            MAC:      mac,
            IP:       best.IP,
            RouterID: best.RouterID,
            Source:   best.Source,
        }

        // 3. Si hay FDB, extraer port
        for _, e := range evs {
            if e.Source == SourceFDB && e.Port != "" {
                device.Port = e.Port
                device.AttachTo = e.RouterID
                break
            }
        }

        // 4. Si hay LLDP neighbor en ese port, actualizar attachTo
        for _, e := range evs {
            if e.Source == SourceLLDP && e.NeighborID != "" {
                device.AttachTo = e.NeighborID
                break
            }
        }

        devices[mac] = device
    }

    return devices, nil
}

// Confidence scoring
func ConfidenceFor(source EvidenceSource) int {
    switch source {
    case SourceFDB:
        return 9  // FDB local = muy confiable
    case SourceLLDP:
        return 10 // LLDP = máxima confianza
    case SourceWiFiAssoc:
        return 8  // WiFi association = confiable
    case SourceARP:
        return 3  // ARP = puede ser indirecto (bridge)
    default:
        return 1
    }
}

type Device struct {
    MAC      string
    IP       string
    RouterID string
    Port     string
    AttachTo string
    Source   EvidenceSource
}
```

### 4.2 FDB Collector (para switch16 u otros dispositivos L2)

```go
// pkg/collector/fdb.go
package collector

import (
    "fmt"
    "net"
    "strings"

    "github.com/vishvananda/netlink"
)

// FDBEntry representa una entrada de la forwarding database
type FDBEntry struct {
    MAC       net.HardwareAddr
    Port      string
    LinkIndex int
    State     int
}

// CollectFDB lee la bridge FDB de un dispositivo Linux (OpenWrt, etc.)
func CollectFDB() ([]FDBEntry, error) {
    neighs, err := netlink.NeighList(0, netlink.FAMILY_ALL)
    if err != nil {
        return nil, fmt.Errorf("neighlist: %w", err)
    }

    links, err := netlink.LinkList()
    if err != nil {
        return nil, fmt.Errorf("linklist: %w", err)
    }

    linkByIndex := make(map[int]netlink.Link)
    for _, l := range links {
        linkByIndex[l.Attrs().Index] = l
    }

    var entries []FDBEntry
    for _, n := range neighs {
        if n.Family != netlink.FAMILY_BRIDGE {
            continue
        }

        link := linkByIndex[n.LinkIndex]
        if link == nil {
            continue
        }

        entries = append(entries, FDBEntry{
            MAC:       n.HardwareAddr,
            Port:      link.Attrs().Name,
            LinkIndex: n.LinkIndex,
            State:     n.State,
        })
    }

    return entries, nil
}

// FilterDirectlyConnected filtra entries que son dispositivos directamente conectados
// (excluye self/permanent entries del propio switch)
func FilterDirectlyConnected(entries []FDBEntry) []FDBEntry {
    var result []FDBEntry
    for _, e := range entries {
        // Skip permanent/self entries
        if e.State == netlink.NUD_PERMANENT || e.State == netlink.NUD_NOARP {
            continue
        }
        // Skip internal bridge ports
        if strings.HasPrefix(e.Port, "br-") {
            continue
        }
        result = append(result, e)
    }
    return result
}
```

### 4.3 LLDP Collector

```go
// pkg/collector/lldp.go
package collector

import (
    "encoding/json"
    "fmt"
    "os/exec"
)

// LLDPNeighbor representa un vecino descubierto por LLDP
type LLDPNeighbor struct {
    LocalPort    string `json:"port"`
    ChassisID    string `json:"chassis-id"`
    PortID       string `json:"port-id"`
    SysName      string `json:"sys-name"`
    SysDescr     string `json:"sys-descr"`
}

// CollectLLDP ejecuta lldpcli y parsea el output JSON
func CollectLLDP() ([]LLDPNeighbor, error) {
    cmd := exec.Command("lldpcli", "show", "neighbors", "-f", "json")
    out, err := cmd.Output()
    if err != nil {
        return nil, fmt.Errorf("lldpcli: %w", err)
    }

    var result struct {
        LLDP struct {
            Interface map[string]struct {
                Via string `json:"via"`
                Port struct {
                    ID struct {
                        Type  string `json:"type"`
                        Value string `json:"value"`
                    } `json:"id"`
                } `json:"port"`
                Chassis map[string]struct {
                    ID struct {
                        Type  string `json:"type"`
                        Value string `json:"value"`
                    } `json:"id"`
                    Descr string `json:"descr"`
                } `json:"chassis"`
            } `json:"interface"`
        } `json:"lldp"`
    }

    if err := json.Unmarshal(out, &result); err != nil {
        return nil, fmt.Errorf("parse lldp: %w", err)
    }

    var neighbors []LLDPNeighbor
    for portName, iface := range result.LLDP.Interface {
        for chassisName, chassis := range iface.Chassis {
            neighbors = append(neighbors, LLDPNeighbor{
                LocalPort: portName,
                ChassisID: chassis.ID.Value,
                PortID:    iface.Port.ID.Value,
                SysName:   chassisName,
                SysDescr:  chassis.Descr,
            })
        }
    }

    return neighbors, nil
}
```

### 4.4 Router Discovery (expandir routerNodes dinámicamente)

```go
// pkg/topology/discovery.go
package topology

import (
    "database/sql"
    "fmt"
)

// RouterDiscovery detecta routers/switches automáticamente
type RouterDiscovery struct {
    db *sql.DB
}

// DiscoverFromLLDP encuentra switches vía LLDP neighbors
func (rd *RouterDiscovery) DiscoverFromLLDP(neighbors []LLDPNeighbor) error {
    for _, n := range neighbors {
        // Si el neighbor es un switch/router conocido, añadirlo
        if isNetworkDevice(n.SysDescr) {
            if err := rd.upsertRouter(n.SysName, n.ChassisID, RoleSwitch); err != nil {
                return err
            }
        }
    }
    return nil
}

// DiscoverFromFDB encuentra switches analizando patrones de FDB
func (rd *RouterDiscovery) DiscoverFromFDB(entries []FDBEntry) error {
    // Si un dispositivo tiene múltiples MACs en su FDB, probablemente es un switch
    macCount := make(map[string]int)
    for _, e := range entries {
        macCount[e.Port]++
    }

    for port, count := range macCount {
        if count > 10 { // Umbral arbitrario
            // Investigar si este port pertenece a un switch
            fmt.Printf("Port %s has %d MACs, possible trunk
", port, count)
        }
    }
    return nil
}

type RouterRole string

const (
    RoleGateway RouterRole = "GW"
    RoleAP      RouterRole = "AP"
    RoleSwitch  RouterRole = "SW"
)

func (rd *RouterDiscovery) upsertRouter(name, mac string, role RouterRole) error {
    _, err := rd.db.Exec(`
        INSERT INTO routers (name, mac, role_badge, discovered_at)
        VALUES (?, ?, ?, datetime('now'))
        ON CONFLICT(mac) DO UPDATE SET
            name = excluded.name,
            role_badge = excluded.role_badge,
            last_seen = datetime('now')
    `, name, mac, string(role))
    return err
}

func isNetworkDevice(sysDescr string) bool {
    // Heurística simple
    keywords := []string{"switch", "router", "bridge", "openwrt", "linux"}
    lower := strings.ToLower(sysDescr)
    for _, kw := range keywords {
        if strings.Contains(lower, kw) {
            return true
        }
    }
    return false
}
```

---

## 5. Fuentes y Referencias

### Artículos y documentación

| Recurso | URL | Relevancia |
|---------|-----|------------|
| NetXMS Topology Discovery | `https://www.netxms.org/topology-discovery/` | Patrón de reconciliación FDB + LLDP |
| WhatsUp Gold Layer 2/3 Discovery | `https://docs.ipswitch.com/NM/WhatsUpGold/2021/01_Guide/WhatsUpGoldLayer2_3Discovery.htm` | SNMP + ARP para topología |
| Go netlink package docs | `https://pkg.go.dev/github.com/vishvananda/netlink` | API para FDB, ARP, interfaces |
| SQLite WAL mode | `https://www.sqlite.org/wal.html` | Documentación oficial WAL |
| Go SQLite best practices | `https://turso.tech/blog/the-definitive-guide-to-sqlite-on-golang` | Concurrency, pragmas, WAL |

### Librerías Go

| Librería | URL | Uso |
|----------|-----|-----|
| `goubus` | `github.com/honeybbq/goubus` | Cliente ubus type-safe para OpenWrt |
| `go-ubus-rpc` | `github.com/daimonaslabs/go-ubus-rpc` | Cliente ubus con CLI |
| `netlink` | `github.com/vishvananda/netlink` | FDB, ARP, interfaces vía netlink |
| `gotopo/lldp` | `github.com/ksang/gotopo/lldp` | Estructuras LLDP en Go |
| `lldp2map` | `github.com/buraglio/lldp2map` | Walk recursivo LLDP + diagramas |
| `gosnmp` | `github.com/gosnmp/gosnmp` | Cliente SNMP v1/v2c/v3 |
| `modernc.org/sqlite` | `modernc.org/sqlite` | SQLite sin CGO |

### Comandos útiles para debugging

```bash
# Ver FDB en OpenWrt
bridge fdb show

# Ver ARP table
ip neigh show

# Ver LLDP neighbors
lldpcli show neighbors

# Ver WiFi associations (OpenWrt)
iwinfo wlan0 assoclist
ubus call hostapd.wlan0 get_clients

# Ver bridge details
brctl show
bridge link show
```

---

*Documento generado para el proyecto NetPulse — https://github.com/gnacho/netpulse*
