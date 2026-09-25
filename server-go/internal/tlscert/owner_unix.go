//go:build unix

package tlscert

import (
	"io/fs"
	"os"
	"syscall"
)

// ownedByMe reports whether fi belongs to the user this process runs as.
func ownedByMe(fi fs.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return !ok || int(st.Uid) == os.Geteuid()
}
