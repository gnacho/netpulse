#!/bin/bash
# Downloads an OpenWrt SDK and verifies it against the target sha256sums
# before extracting it. Sourced by package*.sh.
#
# Usage:
#   SDK_DIR="$(download_verified_sdk "$SDK_DIR_URL" "$WORK_DIR")" || exit 1

download_verified_sdk() {
  local dir_url="$1"
  local work_dir="$2"
  local name sums line sdk_path

  sums="$work_dir/sha256sums"
  if ! curl -fsSL "${dir_url}sha256sums" -o "$sums"; then
    echo "ERROR: no sha256sums under ${dir_url}" >&2
    return 1
  fi

  name="$(curl -fsSL "$dir_url" 2>/dev/null | grep -o 'openwrt-sdk-[^"]*\.tar\.zst' | head -1 || true)"
  if [ -z "$name" ]; then
    echo "ERROR: SDK not found under ${dir_url}" >&2
    return 1
  fi

  line="$(awk -v n="$name" '
    {
      candidate = $NF
      sub(/^\*/, "", candidate)
      if (candidate == n) {
        print
        found = 1
      }
    }
    END { exit found ? 0 : 1 }
  ' "$sums")" || {
    echo "ERROR: ${name} is not listed in sha256sums" >&2
    return 1
  }

  printf '%s\n' "$line" > "$work_dir/sdk.sha256"
  echo "  SDK: ${dir_url}${name}" >&2

  sdk_path="$work_dir/$name"
  if ! curl -fsSL "${dir_url}${name}" -o "$sdk_path"; then
    echo "ERROR: could not download ${name}" >&2
    return 1
  fi

  if ! (cd "$work_dir" && sha256sum -c sdk.sha256) >&2; then
    echo "ERROR: SDK checksum mismatch for ${name}" >&2
    return 1
  fi

  if ! tar --zstd -xf "$sdk_path" -C "$work_dir"; then
    echo "ERROR: could not extract ${name}" >&2
    return 1
  fi

  printf '%s\n' "$work_dir/${name%.tar.zst}"
}
