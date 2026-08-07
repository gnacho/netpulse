#!/bin/sh
# =============================================================================
# NetPulse Collector — one-liner installer (Linux server)
#
#   Sidecar that probes TCP latency/availability to each router (reading the
#   router list from netpulse.db READ-ONLY) and stores its own time-series
#   SQLite (raw → 5-min buckets → daily). Companion of the NetPulse app.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/gnacho/netpulse/main/install-collector.sh | sh
#   sh install-collector.sh --version=1.0.0 --unattended
#   sh install-collector.sh --dry-run
#   sh install-collector.sh --uninstall [--purge]
#
# Requirements: Linux with systemd, amd64 / arm64 / armv7.
# Verifies sha256 of every download (checksums.txt).
#
# Layout:
#   /usr/local/bin/netpulse-collector   binary
#   /var/lib/netpulse-collector/data    metrics.db + state.json
# =============================================================================
set -eu

APP_NAME="netpulse-collector"
GH_REPO="gnacho/netpulse"
BIN_NAME="netpulse-collector"
DEFAULT_PORT="9100"
INSTALL_DIR="/usr/local/bin"
STATE_DIR="/var/lib/$APP_NAME"
SERVICE_NAME="$APP_NAME"
# Where the collector looks for the app's DB (read-only) to learn the routers:
NETPULSE_DB="/var/lib/netpulse/data/netpulse.db"

NETPULSE_VERSION=""; UNATTENDED=0; DRY_RUN=0; UNINSTALL=0; PURGE=0

# ---------------------------------------------------------------- logging ---
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
    C_G=$(printf '\033[32m'); C_R=$(printf '\033[31m'); C_Y=$(printf '\033[33m'); C_B=$(printf '\033[1m'); C_0=$(printf '\033[0m')
else C_G=""; C_R=""; C_Y=""; C_B=""; C_0=""; fi
info()  { printf '%s--%s %s\n' "$C_B" "$C_0" "$*"; }
ok()    { printf '%s✓%s %s\n' "$C_G" "$C_0" "$*"; }
warn()  { printf '%s!%s %s\n' "$C_Y" "$C_0" "$*" >&2; }
err()   { printf '%s✗ %s%s\n' "$C_R" "$*" "$C_0" >&2; }
fatal() { _c=$1; shift; err "$*"; exit "$_c"; }
run()   { if [ "$DRY_RUN" -eq 1 ]; then info "[dry-run] $*"; else "$@"; fi; }

usage() {
    cat <<EOF
NetPulse Collector — installer (TCP latency probes + time-series SQLite)

Usage: sh install-collector.sh [options]
  --version=X.Y.Z   version to install (default: latest stable release)
  --unattended      no questions (automatic when there's no TTY)
  --dry-run         describe each step without touching the system
  --uninstall       remove service, binary and user (keeps $STATE_DIR)
  --purge           with --uninstall: also remove data
  -h, --help        this help

Repo: https://github.com/$GH_REPO
EOF
    exit 0
}

for arg in "$@"; do
    case "$arg" in
        --version=*)  NETPULSE_VERSION="${arg#*=}" ;;
        --unattended) UNATTENDED=1 ;;
        --dry-run)    DRY_RUN=1 ;;
        --uninstall)  UNINSTALL=1 ;;
        --purge)      PURGE=1 ;;
        -h|--help)    usage ;;
        *) fatal 10 "unknown option: $arg (try --help)" ;;
    esac
done
[ -t 0 ] || UNATTENDED=1

# -------------------------------------------------------------- elevation ---
if [ "$(id -u)" -eq 0 ]; then SUDO=""
elif command -v sudo >/dev/null 2>&1; then SUDO="sudo -E"
elif command -v doas >/dev/null 2>&1; then SUDO="doas"
else fatal 22 "I need root (or sudo/doas). Download the script and run it as root."
fi

# --------------------------------------------------------------- uninstall --
if [ "$UNINSTALL" -eq 1 ]; then
    info "uninstalling $APP_NAME"
    if [ -f "/etc/systemd/system/$SERVICE_NAME.service" ]; then
        run $SUDO systemctl stop "$SERVICE_NAME" 2>/dev/null || true
        run $SUDO systemctl disable "$SERVICE_NAME" 2>/dev/null || true
        run $SUDO rm -f "/etc/systemd/system/$SERVICE_NAME.service"
        run $SUDO systemctl daemon-reload
        ok "systemd unit removed"
    fi
    run $SUDO rm -f "$INSTALL_DIR/$BIN_NAME" "$INSTALL_DIR/$BIN_NAME.bak"
    ok "binary removed from $INSTALL_DIR"
    if id "$APP_NAME" >/dev/null 2>&1; then
        run $SUDO userdel "$APP_NAME" 2>/dev/null || warn "could not delete user $APP_NAME"
        ok "system user removed"
    fi
    if [ "$PURGE" -eq 1 ]; then
        run $SUDO rm -rf "$STATE_DIR"
        ok "data removed (--purge)"
    else
        info "data kept in $STATE_DIR (remove with --purge)"
    fi
    ok "$APP_NAME uninstalled"
    exit 0
fi

# --------------------------------------------------------------- detection --
. /etc/os-release 2>/dev/null || true
OS_PRETTY="${PRETTY_NAME:-$(uname -s)}"

ARCH=$(uname -m)
case "$ARCH" in
    x86_64|amd64)   GOARCH=amd64 ;;
    aarch64|arm64)  GOARCH=arm64 ;;
    armv7l|armv7)   GOARCH=armv7 ;;
    *) fatal 20 "unsupported architecture: $ARCH (released: amd64, arm64, armv7)"
esac

if [ ! -d /run/systemd/system ] || ! command -v systemctl >/dev/null 2>&1; then
    fatal 23 "the collector needs systemd (this machine doesn't run it)"
fi
info "detected: $OS_PRETTY · linux/$GOARCH · systemd"

if command -v curl >/dev/null 2>&1; then FETCH="curl -fsSL --retry 3 --connect-timeout 10"
elif command -v wget >/dev/null 2>&1; then FETCH="wget -q -O-"
else fatal 21 "I need curl or wget"
fi
fetch_to() { $FETCH "$1" > "$2"; }
command -v sha256sum >/dev/null 2>&1 || fatal 21 "missing sha256sum (coreutils)"

# ------------------------------------------------------ port pre-flight -----
port_in_use() {
    if command -v ss >/dev/null 2>&1; then
        ss -tln 2>/dev/null | awk '{print $4}' | grep -qE "[:.]${1}\$"
    elif command -v netstat >/dev/null 2>&1; then
        netstat -tln 2>/dev/null | awk '{print $4}' | grep -qE "[:.]${1}\$"
    else
        return 1  # can't check: assume free
    fi
}
pick_port() {
    _p=$1; _end=$((_p + 20))
    while [ "$_p" -le "$_end" ]; do
        if ! port_in_use "$_p"; then printf '%s' "$_p"; return 0; fi
        _p=$((_p + 1))
    done
    return 1
}

# Fresh install only: an upgrade keeps the port from the existing unit.
tty_ok() { (exec 3<>/dev/tty) 2>/dev/null; }

# choose_port WANT — interactive (TTY): asks, suggesting the next free port.
# Non-interactive: prints the next free one. Fails if none is free.
choose_port() {
    _want=$1
    _next=$(pick_port "$((_want + 1))") || _next=""
    if [ "$UNATTENDED" -eq 0 ] && tty_ok; then
        while :; do
            printf 'Port %s is already in use.\nWhich port should the collector listen on? [%s] ' \
                "$_want" "${_next:-none free}" > /dev/tty
            IFS= read -r _r < /dev/tty || _r=""
            _r="${_r:-$_next}"
            case "$_r" in
                ''|*[!0-9]*) printf 'Please enter a port number.\n' > /dev/tty; continue ;;
            esac
            if [ "$_r" -lt 1 ] || [ "$_r" -gt 65535 ]; then
                printf 'Out of range (1-65535).\n' > /dev/tty; continue
            fi
            if port_in_use "$_r"; then
                printf 'Port %s is also in use.\n' "$_r" > /dev/tty; continue
            fi
            printf '%s' "$_r"; return 0
        done
    fi
    [ -n "$_next" ] || return 1
    printf '%s' "$_next"
}

PORT="$DEFAULT_PORT"
if [ ! -f "/etc/systemd/system/$SERVICE_NAME.service" ]; then
    if port_in_use "$DEFAULT_PORT"; then
        PORT=$(choose_port "$DEFAULT_PORT") \
            || fatal 25 "port $DEFAULT_PORT is busy and no free port found in $((DEFAULT_PORT + 1))-$((DEFAULT_PORT + 21))"
        warn "port $DEFAULT_PORT is already in use — the collector will listen on $PORT instead"
    else
        ok "port $DEFAULT_PORT is free"
    fi
fi

# --------------------------------------------------------- resolve version --
if [ -z "$NETPULSE_VERSION" ]; then
    info "resolving latest stable version"
    NETPULSE_VERSION=$($FETCH "https://api.github.com/repos/$GH_REPO/releases/latest" \
        | grep '"tag_name"' | head -1 | cut -d'"' -f4) \
        || fatal 31 "could not resolve the latest version. Use --version=X.Y.Z"
    [ -n "$NETPULSE_VERSION" ] || fatal 31 "no stable release found yet. Use --version=X.Y.Z"
fi
VERSION_NORM=$(echo "$NETPULSE_VERSION" | sed 's/^v//')
case "$NETPULSE_VERSION" in
    v*) NETPULSE_TAG="$NETPULSE_VERSION" ;;
    *)  NETPULSE_TAG="v$NETPULSE_VERSION" ;;
esac
ASSET="${BIN_NAME}_${VERSION_NORM}_linux_${GOARCH}.tar.gz"
BASE_URL="https://github.com/$GH_REPO/releases/download/$NETPULSE_TAG"
info "version: $NETPULSE_VERSION"

$FETCH "https://github.com/$GH_REPO" >/dev/null \
    || fatal 30 "no access to github.com (proxy? DNS? firewall?)"

UPGRADING=0
if [ -x "$INSTALL_DIR/$BIN_NAME" ]; then
    UPGRADING=1
    info "previous install detected: upgrade mode (data is preserved)"
fi

# ---------------------------------------------------------- download+verify --
TMP=$(mktemp -d) || fatal 34 "mktemp failed"
cleanup() { rm -rf "$TMP"; return 0; }
trap cleanup EXIT INT TERM

info "downloading $ASSET"
fetch_to "$BASE_URL/$ASSET" "$TMP/$ASSET" || fatal 32 "download failed: $BASE_URL/$ASSET"
fetch_to "$BASE_URL/checksums.txt" "$TMP/checksums.txt" || fatal 32 "checksums.txt not found in release $NETPULSE_VERSION"
[ -s "$TMP/$ASSET" ] || fatal 32 "downloaded asset is empty"

SUM_FILE=$(sha256sum "$TMP/$ASSET" | awk '{print $1}')
SUM_REF=$(grep "  $ASSET\$" "$TMP/checksums.txt" | awk '{print $1}')
[ -n "$SUM_REF" ] || fatal 33 "$ASSET not listed in checksums.txt"
[ "$SUM_FILE" = "$SUM_REF" ] || fatal 33 "sha256 MISMATCH for $ASSET — corrupt or tampered download"
ok "sha256 verified"

tar -tzf "$TMP/$ASSET" >/dev/null 2>&1 || fatal 34 "tarball is corrupt"
tar -xzf "$TMP/$ASSET" -C "$TMP"
[ -s "$TMP/$BIN_NAME" ] || fatal 34 "tarball does not contain $BIN_NAME"

# ------------------------------------------------------------------ install --
run $SUDO install -d -m 0755 "$INSTALL_DIR"
if [ "$UPGRADING" -eq 1 ]; then
    run $SUDO cp -a "$INSTALL_DIR/$BIN_NAME" "$INSTALL_DIR/$BIN_NAME.bak"
    info "previous binary backed up: $INSTALL_DIR/$BIN_NAME.bak"
fi
run $SUDO install -T -m 0755 "$TMP/$BIN_NAME" "$INSTALL_DIR/$BIN_NAME"
ok "binary installed at $INSTALL_DIR/$BIN_NAME"

if ! id "$APP_NAME" >/dev/null 2>&1; then
    run $SUDO useradd --system --home-dir "$STATE_DIR" --shell /usr/sbin/nologin "$APP_NAME"
    ok "system user $APP_NAME created"
fi
run $SUDO install -d -m 0750 -o "$APP_NAME" -g "$APP_NAME" "$STATE_DIR"

# Read access to the app's DB: join the netpulse group if it exists
if getent group netpulse >/dev/null 2>&1; then
    run $SUDO usermod -a -G netpulse "$APP_NAME" 2>/dev/null || true
    ok "user $APP_NAME added to group netpulse (read access to netpulse.db)"
else
    info "no netpulse group found: set NETPULSE_DB in the unit if the app's DB lives elsewhere"
fi

# SELinux: context fix, degrade to warning
if command -v getenforce >/dev/null 2>&1 && [ "$(getenforce)" = "Enforcing" ]; then
    run $SUDO chcon -t bin_t "$INSTALL_DIR/$BIN_NAME" 2>/dev/null \
        || warn "SELinux Enforcing: if the service fails, check 'ausearch -m avc -ts recent'"
fi

# ----------------------------------------------------------------- service --
if [ "$DRY_RUN" -eq 1 ]; then info "[dry-run] would write systemd unit + enable --now"; else
    $SUDO tee "/etc/systemd/system/$SERVICE_NAME.service" >/dev/null <<EOF
[Unit]
Description=NetPulse Collector — TCP latency probes to routers (time-series)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$APP_NAME
Group=$APP_NAME
Environment=DATA_DIR=$STATE_DIR/data
Environment=NETPULSE_DB=$NETPULSE_DB
Environment=LISTEN=127.0.0.1:$PORT
ExecStart=$INSTALL_DIR/$BIN_NAME
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
PrivateTmp=true
LockPersonality=true
RemoveIPC=true
StateDirectory=$APP_NAME
UMask=007

[Install]
WantedBy=multi-user.target
EOF
fi
run $SUDO systemctl daemon-reload
if [ "$UPGRADING" -eq 1 ]; then run $SUDO systemctl restart "$SERVICE_NAME"
else run $SUDO systemctl enable --now "$SERVICE_NAME"; fi

if [ "$DRY_RUN" -eq 0 ]; then
    sleep 3
    if ! $SUDO systemctl is-active --quiet "$SERVICE_NAME"; then
        err "service failed to start; diagnostics:"
        $SUDO systemctl status "$SERVICE_NAME" --no-pager || true
        $SUDO journalctl -u "$SERVICE_NAME" -n 50 --no-pager || true
        exit 40
    fi
    ok "service $SERVICE_NAME active"
    if command -v curl >/dev/null 2>&1; then
        curl -fsS --max-time 5 "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1 \
            && ok "healthz OK on 127.0.0.1:$PORT" \
            || warn "service is up but healthz didn't answer yet (give it a few seconds)"
    fi
fi

# ------------------------------------------------------------------ summary --
printf '\n%s================ %s installed ================%s\n' "$C_G" "$APP_NAME" "$C_0"
printf 'Version:    %s%s\n' "$NETPULSE_VERSION" "$( [ "$UPGRADING" -eq 1 ] && echo " (upgrade — previous binary at $INSTALL_DIR/$BIN_NAME.bak)" || true)"
printf 'Binary:     %s\n' "$INSTALL_DIR/$BIN_NAME"
printf 'Data:       %s/data (metrics.db, state.json)\n' "$STATE_DIR"
printf 'Health:     http://127.0.0.1:%s/healthz (localhost only)\n' "$PORT"
printf 'Routers DB: %s (read-only)\n' "$NETPULSE_DB"
printf '\nUseful commands:\n'
printf '  systemctl status %s\n  journalctl -u %s -f\n' "$SERVICE_NAME" "$SERVICE_NAME"
printf '  sh install-collector.sh              # update to the latest stable version\n'
printf '  sh install-collector.sh --uninstall\n'
printf '\nNote: the health endpoint binds to localhost only — no firewall changes.\n'
printf '%s================================================%s\n\n' "$C_G" "$C_0"
