#!/bin/sh
# Regenera THIRD_PARTY.md con go-licenses (issue #1170).
# Requiere: go install github.com/google/go-licenses@latest
set -e

cd "$(dirname "$0")/.."

GL="${GO_LICENSES:-go-licenses}"
GL="$(command -v "$GL" 2>/dev/null || echo "$HOME/go/bin/go-licenses")"
if [ ! -x "$GL" ]; then
	echo "go-licenses no encontrado: go install github.com/google/go-licenses@latest" >&2
	exit 1
fi

# go-licenses clasifica modernc.org/mathutil como Unknown, pero su LICENSE es
# BSD-3-Clause (verificado a mano); se corrige aquí al regenerar.
MATHUTIL='| modernc.org/mathutil | BSD-3-Clause¹ | https://gitlab.com/cznic/mathutil/blob/v1.7.1/LICENSE |'

gen() {
	(cd "$1" && "$GL" report ./... --ignore github.com/gnacho/netpulse 2>/dev/null \
		| awk -F',' '{name=$1; lic=$3; url=$2; gsub(/^ +| +$/,"",lic); printf "| %s | %s | %s |\n", name, lic, url}' \
		| grep -v 'modernc.org/mathutil' | sort -u)
	printf '%s\n' "$MATHUTIL"
}

{
	cat <<'HDR'
# Third-Party Licenses

NetPulse itself is licensed under the GNU Affero General Public License v3.0 (see `LICENSE`).

The release binaries ship with the following third-party Go modules compiled in. Their licenses are all permissive (MIT / BSD-2-Clause / BSD-3-Clause / Apache-2.0) and require preserving their copyright notices, which this file collects. Full license texts are available at the linked URLs and in the Go module cache used at build time.

Regenerate with `deploy/gen-third-party.sh` (requires `go install github.com/google/go-licenses@latest`).

## Server (`server-go`)

| Module | License | License URL |
|---|---|---|
HDR
	gen server-go
	cat <<'COL'

## Collector (`collector`)

| Module | License | License URL |
|---|---|---|
COL
	gen collector
	cat <<'AG'

## Agent (`agent`)

| Module | License | License URL |
|---|---|---|
AG
	gen agent
	cat <<'FT'

---

¹ `go-licenses` reports `modernc.org/mathutil` as "Unknown" because its LICENSE file is not recognized by the tool's classifier; the file itself contains the standard BSD-3-Clause text (verified manually).
FT
} > THIRD_PARTY.md

echo "THIRD_PARTY.md regenerado ($(wc -l < THIRD_PARTY.md) lineas)"
