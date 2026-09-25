//go:build !unix

package tlscert

import "io/fs"

// ownedByMe cannot be checked here; the mode checks still apply.
func ownedByMe(fs.FileInfo) bool { return true }
