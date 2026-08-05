/**
 * Adapter live para AdGuard Home (API HTTP /control).
 * Basic auth con ADGUARD_USER/ADGUARD_PASS. Timeout corto y errores controlados:
 * si AdGuard no responde, devolvemos status 'inactive' y el poller sigue.
 *
 * NOTA SANDBOX: sin AdGuard real aquí; código defensivo no probado en vivo.
 */
const HTTP_TIMEOUT_MS = 4000

export class AdGuardClient {
  /** @param {{ url: string, user: string, pass: string }} cfg url sin barra final */
  constructor(cfg) {
    this.url = cfg.url
    this.auth = 'Basic ' + Buffer.from(`${cfg.user}:${cfg.pass}`).toString('base64')
  }

  async _get(path) {
    const ctrl = new AbortController()
    const timer = setTimeout(() => ctrl.abort(), HTTP_TIMEOUT_MS)
    try {
      const res = await fetch(`${this.url}${path}`, {
        headers: { Authorization: this.auth, Accept: 'application/json' },
        signal: ctrl.signal,
      })
      if (!res.ok) throw new Error(`AdGuard ${path} → HTTP ${res.status}`)
      return await res.json()
    } finally {
      clearTimeout(timer)
    }
  }

  /**
   * Devuelve AdGuardStats (shape del contrato). Lanza si AdGuard está caído;
   * el caller decide degradar.
   */
  async getStats() {
    const [status, stats, filtering] = await Promise.all([
      this._get('/control/status'),
      this._get('/control/stats'),
      this._get('/control/filtering/status').catch(() => null),
    ])

    const queries = stats.num_dns_queries ?? 0
    const blocked = stats.num_blocked_filtering ?? 0
    const host = new URL(this.url).hostname
    const port = Number(new URL(this.url).port || 80)

    const filters = filtering?.filters ?? []
    return {
      host,
      port,
      status: status.protection_enabled && status.running ? 'active' : 'inactive',
      queries24h: queries,
      blocked24h: blocked,
      blockedPct: queries > 0 ? +((blocked / queries) * 100).toFixed(1) : 0,
      // AdGuard no separa trackers en /control/stats; aproximación: bloqueos
      // por safebrowsing/parental no incluidos. Mejor esfuerzo.
      trackersBlocked: stats.num_replaced_safebrowsing != null
        ? blocked - (stats.num_replaced_safebrowsing + (stats.num_replaced_parental || 0))
        : blocked,
      // avg_processing_time viene en segundos (float) → ms
      dnsLatencyMs: Math.round((stats.avg_processing_time ?? 0) * 1000),
      clientsUsing: stats.num_clients ?? 0,
      clientsTotal: stats.num_clients ?? 0,
      topBlocked: (stats.top_blocked_domains ?? []).slice(0, 5).map((d) =>
        // versiones nuevas: {domain: count}; viejas: ["domain", count]
        Array.isArray(d)
          ? { domain: d[0], count: d[1] }
          : { domain: Object.keys(d)[0], count: Object.values(d)[0] },
      ),
      filterLists: filters.filter((f) => f.enabled).length,
      rules: filters.reduce((acc, f) => acc + (f.enabled ? f.rules_count || 0 : 0), 0),
    }
  }
}
