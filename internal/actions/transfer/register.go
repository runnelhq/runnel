// Package transfer contains the actions of the namespace "transfer" (ADR 0022, section 6):
// files to and from a server of an sftp, ftps or ftp connection (S57a: transfer.upload,
// transfer.download, transfer.list) and files on it (S57b: transfer.delete, transfer.move,
// transfer.mkdir, transfer.exists, transfer.wait). Options and results follow the file.*
// actions of the same name (ADR 0025): wildcards in the last element, ifExists checked
// before the first write, a list of paths as output, writing via a temporary file and
// renaming it. Names from the server that could lead outside a folder are refused.
//
// Paths on the server are POSIX paths (/ as separator); relative ones refer to the folder
// after the sign-in. Local paths refer to the project folder (ADR 0020). Each execution
// opens one connection and closes it at the end (session.go); there is no pool. The
// protocol of the connection is behind remoteFS: sftp.go for SFTP, ftp.go for FTPS and FTP
// (S57c). Each action lives in a file of its own (<name>.go) with its tests next to it;
// remote.go holds what they share.
package transfer

import "github.com/runnelhq/runnel/internal/action"

// Register adds all actions of the namespace "transfer" to a registry.
func Register(r *action.Registry) {
	r.Register(uploadDefinition)
	r.Register(downloadDefinition)
	r.Register(listDefinition)
	r.Register(deleteDefinition)
	r.Register(moveDefinition)
	r.Register(mkdirDefinition)
	r.Register(existsDefinition)
	r.Register(waitDefinition)
}
