// Package transfer contains the actions of the namespace "transfer" (ADR 0022, section 6):
// files to and from a server of an sftp connection (S57a: transfer.upload,
// transfer.download, transfer.list). Options and results follow the file.* actions of the
// same name (ADR 0025): wildcards in the last element, ifExists checked before the first
// write, a list of paths as output, writing via a temporary file and renaming it.
//
// Paths on the server are POSIX paths (/ as separator); relative ones refer to the folder
// after the sign-in. Local paths refer to the project folder (ADR 0020). Each execution
// opens one SSH connection and closes it at the end (session.go); there is no pool. Each
// action lives in a file of its own (<name>.go) with its tests next to it; remote.go holds
// what they share.
package transfer

import "github.com/runnelhq/runnel/internal/action"

// Register adds all actions of the namespace "transfer" to a registry.
func Register(r *action.Registry) {
	r.Register(uploadDefinition)
	r.Register(downloadDefinition)
	r.Register(listDefinition)
}
