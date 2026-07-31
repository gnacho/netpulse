/**
 * Actualizador: comprueba periódicamente si hay versión nueva en GitHub
 * (rama main del repo) y aplica la actualización ejecutando deploy/update.sh
 * (git pull + deps + build frontend + reinicio diferido del servicio).
 *
 * - Repo privado: necesita GITHUB_TOKEN (fine-grained, Contents: read).
 * - El chequeo corre al arrancar y cada CHECK_INTERVAL_MS, bajo demanda
 *   con POST /api/update/check.
 * - El estado se expone en GET /api/update/status (admin).
 */
import { execFile, spawn } from 'node:child_process'
import fs from 'node:fs'

const CHECK_INTERVAL_MS = 6 * 60 * 60 * 1000 // 6 h
const HTTP_TIMEOUT_MS = 8000

const state = {
  current: 'desconocido',
  latest: null,
  latestMsg: null,
  updateAvailable: false,
  lastCheck: null,
  updating: false, // { step } mientras corre update.sh
  error: null,
}

let timer = null

function git(args, cwd) {
  return new Promise((resolve) => {
    execFile('git', args, { cwd, timeout: 10000 }, (err, stdout) => {
      resolve(err ? '' : stdout.trim())
    })
  })
}

async function currentCommit(repoRoot) {
  const sha = await git(['rev-parse', '--short', 'HEAD'], repoRoot)
  return sha || 'desconocido'
}

/** Consulta el último commit de main en la API de GitHub. */
async function fetchLatest(repo, token) {
  const ctrl = new AbortController()
  const t = setTimeout(() => ctrl.abort(), HTTP_TIMEOUT_MS)
  try {
    const headers = {
      Accept: 'application/vnd.github+json',
      'User-Agent': 'netpulse-updater',
      'X-GitHub-Api-Version': '2022-11-28',
    }
    if (token) headers.Authorization = `Bearer ${token}`
    const res = await fetch(`https://api.github.com/repos/${repo}/commits/main`, { headers, signal: ctrl.signal })
    if (res.status === 401 || res.status === 403 || res.status === 404) {
      return { error: token ? `github_${res.status}` : 'no_token' }
    }
    if (!res.ok) return { error: `github_${res.status}` }
    const data = await res.json()
    return { sha: (data.sha || '').slice(0, 7), msg: (data.commit?.message || '').split('\n')[0].slice(0, 80) }
  } catch (err) {
    return { error: err.name === 'AbortError' ? 'timeout' : 'network' }
  } finally {
    clearTimeout(t)
  }
}

export function createUpdater({ repoRoot, repo, token }) {
  async function check() {
    state.current = await currentCommit(repoRoot)
    const latest = await fetchLatest(repo, token)
    state.lastCheck = Date.now()
    if (latest.error) {
      state.error = latest.error
      // no_token no es un error visible: el banner simplemente no aparece
      return state
    }
    state.error = null
    state.latest = latest.sha
    state.latestMsg = latest.msg
    state.updateAvailable = Boolean(latest.sha && state.current !== 'desconocido' && latest.sha !== state.current)
    if (state.updateAvailable) {
      console.log(`[netpulse] nueva versión disponible: ${state.current} → ${state.latest} (${latest.msg})`)
    }
    return state
  }

  function apply() {
    if (state.updating) return false
    state.updating = { step: 'start', log: '' }
    // Copia el script a /tmp antes de ejecutarlo: el propio update hace
    // `git reset --hard`, que reescribiría el script mientras bash lo lee
    const tmpScript = `${process.env.TMPDIR || '/tmp'}/netpulse-update-${Date.now()}.sh`
    try {
      fs.copyFileSync(`${repoRoot}/deploy/update.sh`, tmpScript)
      fs.chmodSync(tmpScript, 0o755)
    } catch (err) {
      state.updating = false
      state.error = `update_copy_failed`
      console.error('[netpulse] no se pudo copiar update.sh:', err.message)
      return false
    }
    const child = spawn('bash', [tmpScript], {
      cwd: repoRoot,
      env: { ...process.env, NODE_OPTIONS: '--max-old-space-size=400' },
    })
    child.stdout.on('data', (chunk) => {
      const text = chunk.toString()
      state.updating.log = (state.updating.log + text).slice(-4000)
      const m = /STEP:(\w+)/.exec(text)
      if (m) state.updating.step = m[1]
      console.log('[netpulse:update]', text.trim())
    })
    child.stderr.on('data', (chunk) => {
      state.updating.log = (state.updating.log + chunk.toString()).slice(-4000)
    })
    child.on('close', (code) => {
      if (code === 0) {
        state.updating = { step: 'done', log: state.updating.log }
        state.lastLog = state.updating.log
        state.updateAvailable = false
      } else {
        state.lastLog = state.updating?.log ?? null
        state.updating = false
        state.error = `update_exit_${code}`
      }
    })
    return true
  }

  return {
    start() {
      void check()
      timer = setInterval(() => void check(), CHECK_INTERVAL_MS)
      timer.unref()
    },
    stop() {
      if (timer) clearInterval(timer)
    },
    check,
    apply,
    get status() {
      return {
        ...state,
        updating: state.updating ? { step: state.updating.step } : false,
        lastLog: state.updating?.log?.slice(-800) ?? state.lastLog ?? null,
        repo,
        hasToken: Boolean(token),
      }
    },
  }
}
