/**
 * Cliente AdGuard Home en firmware GL.iNet:
 *
 * - La API (puerto 3000) exige cookie `Admin-Token` = sesión de la UI GL.
 * - Login GL (/rpc): challenge → salt+nonce+alg → crypt local → login → sid.
 *   Todo se calcula EN el router vía SSH root (openssl passwd), sin
 *   implementar crypt en Node. La contraseña viaja base64 por el canal SSH.
 * - Credenciales: Ajustes → AdGuard (kv en el servidor; nunca vuelven al navegador).
 * - El sid se cachea y se renueva al primer 401.
 */
import { execFile } from 'node:child_process'
import { sshBaseArgs } from '../sshkey.js'

const SSH_TIMEOUT_MS = 8000
const HTTP_TIMEOUT_MS = 4000

export class AdGuardGlinetClient {
  /**
   * @param {{ host: string, user: string, pass: string, sshKeyPath: string }} cfg
   */
  constructor(cfg) {
    this.host = cfg.host
    this.user = cfg.user
    this.pass = cfg.pass
    this.sshKeyPath = cfg.sshKeyPath
    this.sid = null
  }

  ssh(cmd, timeoutMs = SSH_TIMEOUT_MS) {
    return new Promise((resolve, reject) => {
      execFile(
        'ssh',
        [...sshBaseArgs(this.sshKeyPath), '-o', 'ControlMaster=no', `root@${this.host}`, cmd],
        { timeout: timeoutMs, maxBuffer: 1024 * 1024 },
        (err, stdout, stderr) => {
          if (err) return reject(new Error(`ssh ${this.host}: ${err.message} ${stderr || ''}`.trim()))
          resolve(stdout)
        },
      )
    })
  }

  /** Login GL completo en el router → sid (cookie Admin-Token). */
  async login() {
    const passB64 = Buffer.from(this.pass, 'utf8').toString('base64')
    const script = [
      `PASS=$(echo ${passB64} | base64 -d)`,
      `RESP=$(curl -s -m 5 -X POST http://127.0.0.1/rpc -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"challenge","params":{"username":"${this.user}"}}')`,
      'SALT=$(echo "$RESP" | jsonfilter -e @.result.salt)',
      'NONCE=$(echo "$RESP" | jsonfilter -e @.result.nonce)',
      'ALG=$(echo "$RESP" | jsonfilter -e @.result.alg)',
      'CIPHER=$(openssl passwd -$ALG -salt "$SALT" "$PASS" 2>/dev/null || echo -n "$PASS" | openssl passwd -$ALG -salt "$SALT" -stdin)',
      'HASH=$(echo -n "$CIPHER:$NONCE" | md5sum | cut -d" " -f1)',
      `RESP2=$(curl -s -m 5 -X POST http://127.0.0.1/rpc -H 'Content-Type: application/json' -d "{\\"jsonrpc\\":\\"2.0\\",\\"id\\":1,\\"method\\":\\"login\\",\\"params\\":{\\"username\\":\\"${this.user}\\",\\"hash\\":\\"$HASH\\"}}")`,
      'SID=$(echo "$RESP2" | jsonfilter -e @.result.sid 2>/dev/null)',
      'echo "SID:$SID"',
    ].join(' && ')
    const out = await this.ssh(script)
    const m = /^SID:(\S+)$/m.exec(out)
    if (!m) throw new Error('login GL falló (revisa usuario/contraseña de la UI)')
    this.sid = m[1]
    return this.sid
  }

  async queryStats() {
    const ctrl = new AbortController()
    const timer = setTimeout(() => ctrl.abort(), HTTP_TIMEOUT_MS)
    try {
      const res = await fetch(`http://${this.host}:3000/control/stats`, {
        headers: { Cookie: `Admin-Token=${this.sid}` },
        signal: ctrl.signal,
      })
      if (res.status === 401) throw new Error('401 no autorizado')
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data = await res.json()
      const total = data.num_dns_queries ?? 0
      const blocked = data.num_blocked_filtering ?? 0
      const topBlocked = Array.isArray(data.top_blocked_domains)
        ? data.top_blocked_domains.slice(0, 5).map((d) => {
            const domain = Object.keys(d)[0] ?? ''
            return { domain, count: d[domain] ?? 0 }
          })
        : []
      return {
        host: this.host,
        port: 3000,
        status: 'active',
        queries24h: total,
        blocked24h: blocked,
        blockedPct: total > 0 ? +((blocked / total) * 100).toFixed(1) : 0,
        trackersBlocked: 0,
        dnsLatencyMs: Math.round((data.avg_processing_time ?? 0) * 1000),
        clientsUsing: Array.isArray(data.top_clients) ? data.top_clients.length : 0,
        clientsTotal: Array.isArray(data.top_clients) ? data.top_clients.length : 0,
        topBlocked,
        filterLists: 0,
        rules: 0,
      }
    } finally {
      clearTimeout(timer)
    }
  }

  /** Stats con login/relogin automático. */
  async getStats() {
    if (!this.sid) await this.login()
    try {
      return await this.queryStats()
    } catch (err) {
      if (!/401/.test(err.message)) throw err
      this.sid = null
      await this.login()
      return this.queryStats()
    }
  }
}
