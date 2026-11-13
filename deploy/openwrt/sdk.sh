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

# ipk_data_tar <ipk>: writes the member data.tar.gz of an ipk to stdout.
#
# The ipk container is not always an `ar` archive (#2016): ipkg-build in the
# 24.10 SDK packs debian-binary, data.tar.gz and control.tar.gz as a gzip tar
# (members come as ./data.tar.gz), so `ar p` on it dies with "file format not
# recognized" and the release job failed before uploading anything. Both shapes
# are valid packages, so detect instead of assuming.
ipk_data_tar() {
  local ipk="$1" member listing
  if ar t "$ipk" >/dev/null 2>&1; then
    ar p "$ipk" data.tar.gz
    return
  fi
  listing="$(tar -tzf "$ipk" 2>/dev/null)" || {
    echo "ERROR: $ipk is neither an ar ipk nor a gzip tar one" >&2
    return 1
  }
  for member in ./data.tar.gz data.tar.gz; do
    if printf '%s\n' "$listing" | grep -qx -- "$member"; then
      tar -xzOf "$ipk" "$member"
      return
    fi
  done
  echo "ERROR: no data.tar.gz inside $ipk" >&2
  return 1
}

# verify_ipk_modes <ipk> <path>=<octal-mode>... checks that the packaged files
# carry the expected permission bits inside data.tar.gz (#1701: ipkg-build
# does not fix permissions, and a non-executable /etc/init.d script breaks
# enable/start on the router). Uses ar + tar, both present on CI hosts.
verify_ipk_modes() {
  local ipk="$1"
  shift
  local spec path want got line data_file
  if [ ! -f "$ipk" ]; then
    echo "ERROR: verify_ipk_modes: ipk not found: $ipk" >&2
    exit 1
  fi
  echo "  Verifying packaged file modes in $(basename "$ipk")..."
  # A temp file, not a variable: data.tar.gz is gzip and command substitution
  # would drop its NUL bytes.
  data_file="$(mktemp)"
  if ! ipk_data_tar "$ipk" > "$data_file"; then
    rm -f "$data_file"
    echo "ERROR: verify_ipk_modes: could not read $ipk" >&2
    exit 1
  fi
  for spec in "$@"; do
    path="${spec%%=*}"
    want="${spec##*=}"
    got="$(tar -tvzf "$data_file" 2>/dev/null \
      | awk -v p="$path" '$NF ~ ("(^|/)" p "$") {print $1; exit}')"
    if [ -z "$got" ]; then
      echo "ERROR: $path not found inside the ipk" >&2
      exit 1
    fi
    case "$want" in
      755) line="rwxr-xr-x" ;;
      644) line="rw-r--r--" ;;
      *) echo "ERROR: verify_ipk_modes: unsupported mode $want" >&2; exit 1 ;;
    esac
    if [ "$got" != "-$line" ]; then
      echo "ERROR: $path has mode $got, want -$line ($want)" >&2
      exit 1
    fi
    echo "    $path: $got OK"
  done
  rm -f "$data_file"
}
