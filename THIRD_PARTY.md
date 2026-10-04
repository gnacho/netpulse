# Third-Party Licenses

NetPulse itself is licensed under the GNU Affero General Public License v3.0 (see `LICENSE`).

The release binaries ship with the following third-party Go modules compiled in. Their licenses are all permissive (MIT / BSD-2-Clause / BSD-3-Clause / Apache-2.0) and require preserving their copyright notices, which this file collects. Full license texts are available at the linked URLs and in the Go module cache used at build time.

Regenerate with `deploy/gen-third-party.sh` (requires `go install github.com/google/go-licenses@latest`).

## Server (`server-go`)

| Module | License | License URL |
|---|---|---|
| github.com/araddon/dateparse | MIT | https://github.com/araddon/dateparse/blob/6b43995a97de/LICENSE |
| github.com/dustin/go-humanize | MIT | https://github.com/dustin/go-humanize/blob/v1.0.1/LICENSE |
| github.com/golang-jwt/jwt/v5 | MIT | https://github.com/golang-jwt/jwt/blob/v5.3.1/LICENSE |
| github.com/gonzalop/mq | MIT | https://github.com/gonzalop/mq/blob/v0.9.11/LICENSE |
| github.com/google/jsonschema-go/jsonschema | MIT | https://github.com/google/jsonschema-go/blob/v0.4.2/LICENSE |
| github.com/google/uuid | BSD-3-Clause | https://github.com/google/uuid/blob/v1.6.0/LICENSE |
| github.com/gorilla/websocket | BSD-2-Clause | https://github.com/gorilla/websocket/blob/e064f32e3674/LICENSE |
| github.com/gosnmp/gosnmp | BSD-2-Clause | https://github.com/gosnmp/gosnmp/blob/v1.45.0/LICENSE |
| github.com/mark3labs/mcp-go | MIT | https://github.com/mark3labs/mcp-go/blob/v1.1.1/LICENSE |
| github.com/m-lab/go | Apache-2.0 | https://github.com/m-lab/go/blob/v0.1.76/LICENSE |
| github.com/m-lab/locate/api | Apache-2.0 | https://github.com/m-lab/locate/blob/v0.19.0/LICENSE |
| github.com/m-lab/ndt7-client-go | Apache-2.0 | https://github.com/m-lab/ndt7-client-go/blob/v0.10.1/LICENSE |
| github.com/m-lab/ndt-server | Apache-2.0 | https://github.com/m-lab/ndt-server/blob/v0.25.0/LICENSE |
| github.com/m-lab/tcp-info | Apache-2.0 | https://github.com/m-lab/tcp-info/blob/v1.9.0/LICENSE |
| github.com/remyoudompheng/bigfft | BSD-3-Clause | https://github.com/remyoudompheng/bigfft/blob/24d4a6f8daec/LICENSE |
| github.com/santhosh-tekuri/jsonschema/v6 | Apache-2.0 | https://github.com/santhosh-tekuri/jsonschema/blob/v6.0.2/LICENSE |
| github.com/SherClockHolmes/webpush-go | MIT | https://github.com/SherClockHolmes/webpush-go/blob/v1.4.0/LICENSE |
| github.com/showwin/speedtest-go/speedtest | MIT | https://github.com/showwin/speedtest-go/blob/v1.8.3/LICENSE |
| github.com/spf13/cast | MIT | https://github.com/spf13/cast/blob/v1.7.1/LICENSE |
| github.com/yosida95/uritemplate/v3 | BSD-3-Clause | https://github.com/yosida95/uritemplate/blob/v3.0.2/LICENSE |
| golang.org/x/crypto | BSD-3-Clause | https://cs.opensource.google/go/x/crypto/+/v0.57.0:LICENSE |
| golang.org/x/net | BSD-3-Clause | https://cs.opensource.google/go/x/net/+/v0.59.0:LICENSE |
| golang.org/x/sys/unix | BSD-3-Clause | https://cs.opensource.google/go/x/sys/+/v0.48.0:LICENSE |
| golang.org/x/text | BSD-3-Clause | https://cs.opensource.google/go/x/text/+/v0.42.0:LICENSE |
| modernc.org/libc | MIT | https://gitlab.com/cznic/libc/blob/v1.77.1/LICENSE-3RD-PARTY.md |
| modernc.org/memory | BSD-3-Clause | https://gitlab.com/cznic/memory/blob/v1.12.1/LICENSE-GO |
| modernc.org/sqlite | BSD-3-Clause | https://gitlab.com/cznic/sqlite/blob/v1.60.1/LICENSE |
| modernc.org/mathutil | BSD-3-Clause¹ | https://gitlab.com/cznic/mathutil/blob/v1.7.1/LICENSE |

## Collector (`collector`)

| Module | License | License URL |
|---|---|---|
| github.com/dustin/go-humanize | MIT | https://github.com/dustin/go-humanize/blob/v1.0.1/LICENSE |
| github.com/google/uuid | BSD-3-Clause | https://github.com/google/uuid/blob/v1.6.0/LICENSE |
| github.com/remyoudompheng/bigfft | BSD-3-Clause | https://github.com/remyoudompheng/bigfft/blob/24d4a6f8daec/LICENSE |
| golang.org/x/sys/unix | BSD-3-Clause | https://cs.opensource.google/go/x/sys/+/v0.48.0:LICENSE |
| modernc.org/libc | MIT | https://gitlab.com/cznic/libc/blob/v1.77.1/LICENSE-3RD-PARTY.md |
| modernc.org/memory | BSD-3-Clause | https://gitlab.com/cznic/memory/blob/v1.12.1/LICENSE-GO |
| modernc.org/sqlite | BSD-3-Clause | https://gitlab.com/cznic/sqlite/blob/v1.60.1/LICENSE |
| modernc.org/mathutil | BSD-3-Clause¹ | https://gitlab.com/cznic/mathutil/blob/v1.7.1/LICENSE |

## Agent (`agent`)

| Module | License | License URL |
|---|---|---|
| golang.org/x/net/dns/dnsmessage | BSD-3-Clause | https://cs.opensource.google/go/x/net/+/v0.59.0:LICENSE |
| modernc.org/mathutil | BSD-3-Clause¹ | https://gitlab.com/cznic/mathutil/blob/v1.7.1/LICENSE |

---

¹ `go-licenses` reports `modernc.org/mathutil` as "Unknown" because its LICENSE file is not recognized by the tool's classifier; the file itself contains the standard BSD-3-Clause text (verified manually).
