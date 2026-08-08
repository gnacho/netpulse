# SPEC — Fase 10: Escritura y orquestación

> Estado: borrador de diseño. Objetivo: NetPulse pasa de panel de
> observación a herramienta declarativa para actuar sobre la red de forma
> segura, reversible y auditable.
> Precedentes: Fases 1-9 (solo lectura + alertas + on-box + TLS + pairing).
> Reglas de diseño: ver `docs/AUDITORIA-FASE65.md` §5.

## 0. Contexto verificado

- **Canal bidireccional**: el SSE del agente (Fase 7.3) ya funciona — el
  servidor envía comandos (`refresh`) y el agente los ejecuta. Fase 10
  añade un comando `apply` que lleva operaciones UCI/servicio.
- **HMAC**: la ingesta del agente va firmada desde Fase 6. El SSE usa
  Bearer (token de agente). Las órdenes de escritura requieren la MISMA
  autenticación — ningún canal nuevo.
- **UCI transaccional**: OpenWrt tiene `uci` con staged changes + commit.
  `uci export` / `uci import` permiten snapshots atómicos. Esto es la
  base del rollback: no se inventa nada, se usa el mecanismo nativo.
- **El agente corre como root** en el router (necesario para ubus/netlink).
  El ejecutor es parte del agente (no un binario separado).

## 1. Principios innegociables

1. **Plan → Apply → State** (patrón Terraform): NUNCA se ejecuta nada sin
   mostrar primero el diff y recibir confirmación explícita del admin.
2. **Idempotencia**: cada operación declara el estado deseado; re-ejecutar
   un plan no duplica efectos (estilo Ansible, no estilo script bash).
3. **Snapshot antes de Apply**: `uci export` de las secciones afectadas +
   backup de cualquier fichero tocado. Sin snapshot verificable, no se aplica.
4. **Healthcheck post-Apply**: si el router deja de responder (ping/WiFi),
   rollback automático en < 60 s.
5. **Allowlist estricta**: el agente SOLO ejecuta operaciones de una lista
   cerrada (UCI set/delete/commit, service restart, opkg install). Shell
   libre está prohibido.
6. **Auditoría append-only**: cada plan, apply y rollback queda en una tabla
   `orchestr_audit` (quién, qué, cuándo, resultado).

## 2. Modelo de recursos

Un **recurso** es una unidad de configuración declarativa con:

```go
type Resource interface {
    Type() string                              // "adguard", "wireguard_peer", ...
    ID() string                                // identificador único del recurso
    DesiredState() any                         // estado declarado por el admin
    CurrentState(agent Executor) (any, error)  // estado real en el router
    Operations(desired, current any) []Op      // diff → lista de operaciones
}
```

Una **Operación** (`Op`) es una primitiva allowlisteada:

```go
type Op struct {
    Kind   string            // "uci_set" | "uci_delete" | "uci_commit" | "service" | "install"
    Args   map[string]string // argumentos validados contra el allowlist
    Desc   string            // descripción humana para el diff ("Set AdGuard DNS upstream")
}
```

### Allowlist de operaciones (cerrada, sin shell libre)

| Kind | Args válidos | Efecto |
|---|---|---|
| `uci_set` | `config`, `section`, `option`, `value` | `uci set <config>.<section>.<option>=<value>` |
| `uci_delete` | `config`, `section`, `option` | `uci delete <config>.<section>.<option>` |
| `uci_add_list` | `config`, `section`, `option`, `value` | `uci add_list ...` |
| `uci_commit` | `config` | `uci commit <config>` (persiste staged changes) |
| `service` | `name`, `action` (restart\|reload\|enable\|disable) | `/etc/init.d/<name> <action>` |
| `install` | `package` (de la allowlist de paquetes) | `opkg install <package>` |

**Validación**: cada arg se valida contra un patrón estricto (regex alfanumérico
+ guiones/puntos, sin shell metachars). Un arg que no casa con el patrón →
rechazo inmediato, sin ejecución.

## 3. Ciclo de vida Plan → Apply → State

```
[Admin declara estado deseado en la UI]
    ↓
POST /api/plans {router: "gateway", resource: "adguard", desired: {...}}
    ↓
Server: compara desired vs current (vía agente SSE → executor → read)
    ↓
Server: genera diff → [Op1, Op2, Op3, ...] + snapshot target
    ↓
GET /api/plans/{id} → el admin revisa el diff
    ↓
POST /api/plans/{id}/apply → confirmación explícita
    ↓
Agent executor: snapshot (uci export) → ejecuta Ops secuencialmente → commit
    ↓
Agent executor: healthcheck (ping gateway + WiFi assoc)
    ↓
OK → estado "applied" + auditoría
FAIL → rollback automático (uci import snapshot) → estado "rolled_back" + alerta
```

### Estados de un plan

```
pending → applied | failed | rolled_back
```

- `pending`: plan generado, esperando confirmación.
- `applied`: todas las Ops ejecutadas + healthcheck OK.
- `failed`: alguna Op falló (sin llegar a commit). Snapshot no necesario.
- `rolled_back`: Ops aplicadas + commit + healthcheck FAIL → rollback automático.

## 4. Ejecutor del agente (sandboxed)

El ejecutor vive dentro del agente (mismo binario, mismo proceso procd).
Recibe el comando `apply` por SSE con la lista de Ops + snapshot target.

```go
// agent/internal/executor/executor.go
type Executor struct {
    allowlist map[string]*OpSpec  // kind → spec con validación de args
}

func (e *Executor) Apply(ops []Op, snapshotKey string) ApplyResult {
    // 1. Snapshot: uci export de los configs afectados
    snap := e.snapshot(affectedConfigs(ops))

    // 2. Ejecutar Ops secuencialmente (staged changes, sin commit aún)
    for _, op := range ops {
        if err := e.validate(op); err != nil {
            return ApplyResult{Status: "failed", Error: err, Op: op.Desc}
        }
        if err := e.execute(op); err != nil {
            e.rollbackStaged()  // uci revert de todo lo staged
            return ApplyResult{Status: "failed", Error: err, Op: op.Desc}
        }
    }

    // 3. Commit (persiste todos los staged changes de golpe)
    for _, cfg := range affectedConfigs(ops) {
        e.commit(cfg)
    }

    // 4. Healthcheck: ¿el router sigue accesible?
    if !e.healthcheck() {
        // ROLLBACK: importar el snapshot
        e.restoreSnapshot(snap)
        return ApplyResult{Status: "rolled_back", Reason: "healthcheck_failed"}
    }

    return ApplyResult{Status: "applied"}
}
```

### Healthcheck

1. `ping -c 1 -W 2 <gateway-IP>` (si este router es un AP)
2. `ping -c 1 -W 2 1.1.1.1` (WAN, si este es el gateway)
3. `iwinfo wlan0 assoc` o `ubus call wireless status` (WiFi arriba)

Si cualquiera falla → rollback. El healthcheck se ejecuta 5 s después del
commit (dar tiempo a que los servicios reinicien). Timeout total: 30 s.

### Snapshot

```sh
# Antes del apply:
uci export dhcp > /tmp/netpulse-snap-<key>.dhcp
uci export network > /tmp/netpulse-snap-<key>.network
# ...por cada config afectado

# Rollback:
cat /tmp/netpulse-snap-<key>.dhcp | uci import
uci commit dhcp
# ...por cada config afectado
```

Los snapshots viven en `/tmp` (RAM, se pierden al reboot — correcto: tras
un reboot el estado es el persistido por commit, que ya pasó healthcheck).

## 5. API del servidor

```
POST   /api/plans              — generar plan (desired state → diff)
GET    /api/plans/{id}         — ver plan + diff + estado
POST   /api/plans/{id}/apply   — aplicar (confirmación explícita)
POST   /api/plans/{id}/rollback — rollback manual (admin)
GET    /api/audit              — log de auditoría (paginado)
```

Todas las rutas son **admin-only** (`RequireAdmin`).

### Schema SQLite

```sql
CREATE TABLE IF NOT EXISTS orchestr_plans (
  id         TEXT PRIMARY KEY,   -- UUID
  router_id  TEXT NOT NULL,      -- slug del router objetivo
  resource   TEXT NOT NULL,      -- "adguard", "wireguard_peer", ...
  desired    TEXT NOT NULL,      -- JSON estado deseado
  diff       TEXT NOT NULL,      -- JSON lista de Ops
  status     TEXT NOT NULL,      -- pending|applied|failed|rolled_back
  created_by TEXT NOT NULL,      -- usuario admin
  created_at INTEGER NOT NULL,   -- unix epoch
  applied_at INTEGER,
  result     TEXT                -- JSON resultado (error, healthcheck, etc.)
);

CREATE TABLE IF NOT EXISTS orchestr_audit (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  plan_id    TEXT NOT NULL,
  action     TEXT NOT NULL,      -- plan|apply|rollback|healthcheck
  actor      TEXT NOT NULL,      -- usuario o "system" (auto-rollback)
  detail     TEXT,               -- JSON
  ts         INTEGER NOT NULL    -- unix epoch
);
```

## 6. Módulos por orden de riesgo/beneficio

### 6.1 AdGuard Home (riesgo bajo, beneficio alto)

**Estado deseado**: `{ enabled: true, port: 3001, upstream: "1.1.1.1" }`

**Operaciones** (plan generado):
1. `install` AdGuard Home (opkg o binario + init script)
2. `uci_set` dhcp.dnsmasq.no_resolv=1 (no usar /etc/resolv.conf)
3. `uci_set` dhcp.dnsmasq.server=127.0.0.1#3001 (DNS hacia AdGuard)
4. `service` adguard restart
5. `service` dnsmasq restart

**Rollback**: revertir dhcp.dnsmasq.server al valor previo + parar AdGuard.

**Healthcheck**: ping a 127.0.0.1#3001 (DNS responde) + ping WAN.

**Riesgo**: si AdGuard no arranca, dnsmasq pierde DNS upstream → sin
resolución. El healthcheck detecta esto (ping WAN por IP funciona, pero
DNS no). Rollback restaura el DNS anterior.

### 6.2 WireGuard peers (riesgo medio)

**Estado deseado**: `{ interface: "wg0", peers: [{pubkey, allowed_ips, name}] }`

**Operaciones**:
1. `uci_set` network.wg0.* (interfaz, si no existe)
2. Por cada peer: `uci_set` network.wgpeer<N>.* (clave, IP, allowed_ips)
3. `uci_commit` network
4. `service` network reload

**Riesgo**: tocar firewall puede aislar el túnel. El peer ya no conecta
si se borra la ruta. El healthcheck verifica que el túnel existente sigue
up (wg show). No se borra el peer actual del admin (protección anti-lockout).

### 6.3 DAWN / 802.11r (riesgo alto) — FUTURO

Requiere coordinación multi-AP (todos los APs deben tener la misma config
802.11r). Fuera del alcance inicial de Fase 10.

### 6.4 Batman-adv (riesgo muy alto) — FUTURO

Mesh sobre LAN física → bucles. Dry-run obligatorio. No en Fase 10 inicial.

## 7. Frontend

Nueva sección en la app (solo admin): **Orquestación**.

- **Por módulo**: tarjeta por recurso (AdGuard, WireGuard, ...).
- **Estado actual**: leído del router (poll o SSE).
- **Estado deseado**: formulario declarativo (switches, inputs, tabla de peers).
- **Plan**: al cambiar el estado deseado → `POST /api/plans` → muestra diff.
- **Apply**: botón que confirma + barra de progreso (cada Op ejecutándose).
- **Resultado**: verde (applied) / rojo (rolled_back) + log.

## 8. Seguridad

- **Auth**: todas las rutas de orquestación son `RequireAdmin`.
- **Origen**: el plan se aplica a través del SSE del agente (ya autenticado
  con token + HMAC). El servidor no abre ningún canal nuevo al router.
- **Allowlist**: el ejecutor del agente rechaza cualquier Op que no esté en
  la lista cerrada. Los args se validan con regex (sin shell metachars).
- **Rate limit**: un apply por router a la vez (mutex en el servidor).
- **Timeout**: si el agente no responde al apply en 60 s → timeout + alerta.

## 9. Fuera de alcance (explícito)

- Batman-adv, DAWN/802.11r coordinado multi-AP (futuro).
- Escritura directa sin plan (todo pasa por plan→apply).
- Shell libre en el router.
- Orquestación multi-router atómica (cada router es independiente).

## 10. Orden de implementación

1. **Agent executor** (`agent/internal/executor/`): allowlist + snapshot +
   healthcheck + rollback. Testeable en local con un router real.
2. **Server plan/apply** (`server-go/internal/orchestr/`): resource model,
   plan generation, API endpoints, audit log.
3. **AdGuard module**: primer módulo end-to-end (recurso + plan + apply).
4. **Frontend orquestación**: UI para AdGuard.
5. **WireGuard module**: segundo módulo.
