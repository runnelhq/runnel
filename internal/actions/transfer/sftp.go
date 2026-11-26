package transfer

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// sftpFS is a server of an sftp connection (session.go dials it).
type sftpFS struct {
	ssh         *ssh.Client
	sftp        *sftp.Client
	posixRename bool
}

func (f *sftpFS) Stat(p string) (fs.FileInfo, error)        { return f.sftp.Stat(p) }
func (f *sftpFS) Lstat(p string) (fs.FileInfo, error)       { return f.sftp.Lstat(p) }
func (f *sftpFS) ReadDir(dir string) ([]fs.FileInfo, error) { return f.sftp.ReadDir(dir) }
func (f *sftpFS) MkdirAll(dir string) error                 { return f.sftp.MkdirAll(dir) }
func (f *sftpFS) Chtimes(p string, t time.Time)             { _ = f.sftp.Chtimes(p, t, t) }
func (f *sftpFS) Remove(p string) error                     { return f.sftp.Remove(p) }
func (f *sftpFS) RemoveDir(p string) error                  { return f.sftp.RemoveDirectory(p) }
func (f *sftpFS) Getwd() (string, error)                    { return f.sftp.Getwd() }
func (f *sftpFS) Lost(err error) bool                       { return errors.Is(err, sftp.ErrSSHFxConnectionLost) }

// Create writes a new file; an existing one is an error. With a local file as r the
// library writes several parts at once.
func (f *sftpFS) Create(p string, r io.Reader) error {
	out, err := f.sftp.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return err
	}
	if _, err := out.ReadFrom(r); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// Retrieve reads a file into w, several parts at once.
func (f *sftpFS) Retrieve(p string, w io.Writer) error {
	in, err := f.sftp.Open(p)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	_, err = in.WriteTo(w)
	return err
}

// Rename renames atomically where the server can (posix-rename@openssh.com), otherwise
// it removes an existing target first if replace is set.
func (f *sftpFS) Rename(from, to string, replace bool) error {
	if f.posixRename {
		return f.sftp.PosixRename(from, to)
	}
	if replace {
		if err := f.sftp.Remove(to); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return f.sftp.Rename(from, to)
}

func (f *sftpFS) Close() {
	if f.sftp != nil {
		_ = f.sftp.Close()
	}
	if f.ssh != nil {
		_ = f.ssh.Close()
	}
}
