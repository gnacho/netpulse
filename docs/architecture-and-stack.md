# Netpulse: Análisis Arquitectónico y Guía de Mejoras de Stack

## 1. Diagnóstico y Decisión Arquitectónica (Opciones A/B/C)

El problema de raíz radica en cómo el colector asigna el `routerId` basándose en qué router reporta la MAC en su tabla ARP, en lugar de basarse en la topología física. En topologías bridged (rt2 ↔ switch16), el ARP de rt2 ve TODO el segmento L2, incluyendo dispositivos detrás de switch16.

### Recomendación: Opción C (con un matiz importante)

El fix A (corrección en el servidor usando FDB/LLDP) **no es solo un parche — es la corrección arquitectónica necesaria**. El colector no puede saber con certeza dónde cuelga un dispositivo cableado solo desde su ARP local.

El contrato correcto agente→server a medio plazo (Opción B) debería ser: *"reporto asociaciones WiFi mías + vecino LLDP directo + FDB de mis propios puertos"*. No "todo lo que veo en ARP".

Mientras tanto, la Opción A permite descifrar la topología en el servidor cruzando FDB de switch16 + LLDP.

**Bug aparte urgente:** El límite hardcoded de 4 routerNodes (gateway + 3 APs) que excluye a switch16 con `roleBadge=SW` debe ser eliminado.

---

## 2. Recursos y Skills Curadas para el Stack

### 2.1 Descubrimiento de Topología de Red (El núcleo del bug)

| Recurso | Por qué sirve / Cómo usarlo |
|---|---|
| **`lldpd`** — [github.com/lldpd/lldpd](https://github.com/lldpd/lldpd) | Implementación de referencia de IEEE 802.1AB. Permite entender qué campos exportar del agente (ChassisID, PortID, TTL). Su cliente `lldpcli` tiene salida JSON. |
| **Linux `bridge fdb show` + `bridge -j`** | JSON estructurado del FDB (Forwarding DataBase). Esto es lo que el agente en switch16 debe reportar al servidor para que el cruce de topología funcione. Doc: `man 8 bridge`. |
| **`ip -j neigh show`** | ARP en JSON. Asegurar que el agente lo usa con este formato para estandarizar. |
| **OpenWrt `ubus`** — [openwrt.org/docs/techref/ubus](https://openwrt.org/docs/techref/ubus) | Si redmi-ax6 y rt2 corren OpenWrt, `ubus call network.device status` da datos canónicos sin parsear texto. |
| **RFC 8174 / IEEE 802.1AB-2016** | Especificación LLDP. Especialmente §7 (TLVs). |

**Patrón arquitectónico a estudiar:** **`ntopng` + `nProbe`** — [github.com/ntop/ntopng](https://github.com/ntop/ntopng). Su collector de capa 2 cruza FDB+LLDP+ARP y construye topología. Sus algoritmos (resolve_bridge_port, etc.) son trasladables a TS/JS.

### 2.2 Patrones de Arquitectura Agente→Servidor

| Recurso | Lección para Netpulse |
|---|---|
| **Telegraf** — [github.com/influxdata/telegraf](https://github.com/influxdata/telegraf) | Su división input/processor/aggregator separa los datos crudos del host (FDB, LLDP) de la inferencia. El "routerId assignment" debe ir en el servidor, no en el agente. |
| **Prometheus `node_exporter`** — [github.com/prometheus/node_exporter](https://github.com/prometheus/node_exporter) | Cada collector es independiente y reporta solo hechos locales del host. Nunca infiere topología. Esa es la disciplina que el agente de Netpulse necesita. |
| **Sensu Go** — [github.com/sensu/sensu-go](https://github.com/sensu/sensu-go) | Modelo de contrato agente→backend con agentes ligeros en routers: "agent solo reporta, backend decide". |

### 2.3 Inferencia de Topología (Algoritmos para el servidor)

Para implementar la Opción A, el servidor necesita:

- **Algoritmo de Spanning Tree inverso**: reconstruir el árbol cruzando BPDU de STP + FDB.
- **Cross-referencing FDB multi-switch**: si switch16 ve MAC X en puerto P5, y rt2 ve MAC X en su ARP, entonces X cuelga de switch16:P5, no de rt2.
- **Paper de referencia**: *"Inferring Network Topology from Layer-2 and Layer-3 Information"* — Chua & Kolaczyk (2006). Define el algoritmo canónico.
- **Prototipado**: Usar `scapy` + `scapy-lldp` para probar el parser antes de integrarlo al agente.

### 2.4 Stack TS/JS del Servidor

| Skill / Recurso | Uso |
|---|---|
| **`awesome-nodejs`** — [github.com/sindresorhus/awesome-nodejs](https://github.com/sindresorhus/awesome-nodejs) | Lista curada general. Filtrar por "network" y "streams". |
| **`fastify`** | Mejor que Express para endpoints como `/api/overview`. Da Schema validation de JSON Schema gratis, útil para validar el reporte del agente. |
| **`cytoscape.js`** — [github.com/cytoscape/cytoscape.js](https://github.com/cytoscape/cytoscape.js) | Para visualizar la topología real durante debugging. Sus layouts `breadthfirst` permiten validar que el cruce FDB+LLDP da el árbol correcto. |
| **`json-schema`** validation | Base para definir el contrato agente→server formalmente y que la evolución del colector no rompa el servidor. |

### 2.5 Testing Reproducible

- **`containerlab`** — [github.com/srl-labs/containerlab](https://github.com/srl-labs/containerlab): Levanta topologías L2/L3 realistas con containers. Permite simular rt2+switch16+CTs sin hardware real.
- **`batfish`** — [github.com/batfish/batfish](https://github.com/batfish/batfish): Su modelo de topología es instructivo para validar configuraciones de red.

---

## 3. Plan de Acción Ejecutable

1. **Inmediato:** Quitar el límite hardcoded de 4 routerNodes. switch16 debe entrar en el cálculo con `roleBadge=SW`.
2. **Esta semana (Fix A - Servidor):**
   - Agente en switch16 reporta `bridge -j fdb show` + `lldpcli show neighbors -f json`.
   - Servidor cruza FDB(switch16) × ARP(rt2) → reasigna `routerId` y `attachTo` de los dispositivos cableados mal asignados.
3. **Medio plazo (Fix B - Contrato Agente):**
   - Redactar el JSON Schema del contrato agente→server.
   - El agente deja de reportar ARP como fuente de "tengo esto colgando". ARP solo se usa en el servidor como pista secundaria.
4. **Documentación del algoritmo:** Capturar el conocimiento implícito de la topología en documentación reproducible dentro del repo.
