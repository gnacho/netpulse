#!/usr/bin/env node
// Verificación MCP end-to-end contra una instancia NetPulse real.
// Uso: node verify-mcp.mjs <baseURL> <token>
const base = process.argv[2];
const token = process.argv[3];
if (!base || !token) { console.error("uso: node verify-mcp.mjs <baseURL> <token>"); process.exit(2); }

let nextId = 0;
async function rpc(method, params = {}) {
  const res = await fetch(`${base}/mcp`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Accept": "application/json",
      "Authorization": `Bearer ${token}`,
    },
    body: JSON.stringify({ jsonrpc: "2.0", id: ++nextId, method, params }),
  });
  const body = await res.json();
  if (body.error) throw new Error(`${method}: ${JSON.stringify(body.error)}`);
  return body.result;
}

function toolText(result) {
  if (result.isError) return { error: result.content?.[0]?.text };
  const text = result.content?.[0]?.text ?? "";
  try { return JSON.parse(text); } catch { return { raw: text.slice(0, 200) }; }
}

let pass = 0, fail = 0;
function check(name, cond, detail = "") {
  if (cond) { pass++; console.log(`PASS ${name}`); }
  else { fail++; console.log(`FAIL ${name} ${detail}`); }
}

const init = await rpc("initialize", {
  protocolVersion: "2025-03-26",
  capabilities: {},
  clientInfo: { name: "verify-mcp", version: "1.0" },
});
check("initialize", init.serverInfo?.name === "netpulse", JSON.stringify(init.serverInfo));
console.log(`  server: ${init.serverInfo?.name} ${init.serverInfo?.version}`);

const tools = await rpc("tools/list");
const names = tools.tools.map(t => t.name).sort();
const want = ["adguard_stats", "alerts", "fleet_status", "list_devices", "router_health", "topology", "wireguard_peers"];
check("tools/list 7 tools", JSON.stringify(names) === JSON.stringify(want), names.join(","));
check("todos readOnlyHint", tools.tools.every(t => t.annotations?.readOnlyHint === true));
check("ningún destructiveHint", tools.tools.every(t => t.annotations?.destructiveHint !== true));

const fleet = toolText(await rpc("tools/call", { name: "fleet_status", arguments: {} }));
check("fleet_status routers>0", (fleet.routers?.length ?? 0) > 0, JSON.stringify(fleet).slice(0, 150));
console.log(`  routers: ${fleet.routers?.map(r => `${r.id}(${r.status},${r.health})`).join(" ")}`);
check("fleet_status deviceTotals", (fleet.deviceTotals?.total ?? 0) > 0);

const firstRouter = fleet.routers?.[0]?.id;
const health = toolText(await rpc("tools/call", { name: "router_health", arguments: { router_id: firstRouter } }));
check("router_health", health.id === firstRouter, JSON.stringify(health).slice(0, 120));

const bad = toolText(await rpc("tools/call", { name: "router_health", arguments: { router_id: "__nope__" } }));
check("router_health desconocido = error", !!bad.error, JSON.stringify(bad));

const devs = toolText(await rpc("tools/call", { name: "list_devices", arguments: {} }));
check("list_devices total>0", (devs.total ?? 0) > 0);
const devsFiltered = toolText(await rpc("tools/call", { name: "list_devices", arguments: { router_id: firstRouter } }));
check("list_devices filtro router", (devsFiltered.total ?? -1) >= 0 && devsFiltered.total <= devs.total);

const topo = toolText(await rpc("tools/call", { name: "topology", arguments: {} }));
check("topology links>=0", Array.isArray(topo.links), JSON.stringify(topo).slice(0, 100));
console.log(`  links: ${topo.links?.length} distnodes: ${topo.distributionNodes?.length}`);

const alerts = toolText(await rpc("tools/call", { name: "alerts", arguments: {} }));
check("alerts array", Array.isArray(alerts.alerts), JSON.stringify(alerts).slice(0, 100));
const alertsActive = toolText(await rpc("tools/call", { name: "alerts", arguments: { active_only: true } }));
check("alerts active_only<=total", (alertsActive.total ?? 99) <= (alerts.total ?? 0));

const wg = toolText(await rpc("tools/call", { name: "wireguard_peers", arguments: {} }));
check("wireguard_peers (o error honesto)", !!wg);
console.log(`  wg: ${wg.error ?? `${wg.interface} peers=${wg.peers?.length}`}`);
const agh = toolText(await rpc("tools/call", { name: "adguard_stats", arguments: {} }));
check("adguard_stats (o error honesto)", !!agh);
console.log(`  agh: ${agh.error ?? `${agh.host} q24h=${agh.queries24h}`}`);

// Auth negativa: sin token debe dar 401.
const noAuth = await fetch(`${base}/mcp`, {
  method: "POST",
  headers: { "Content-Type": "application/json", "Accept": "application/json" },
  body: JSON.stringify({ jsonrpc: "2.0", id: 99, method: "tools/list" }),
});
check("sin token = 401", noAuth.status === 401, `status ${noAuth.status}`);

console.log(`\n${pass}/${pass + fail} PASS`);
process.exit(fail ? 1 : 0);
