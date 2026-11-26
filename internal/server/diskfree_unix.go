//go:build linux || darwin

package server

import "golang.org/x/sys/unix"

// diskFree returns the bytes free for the user and the size of the disk of dir.
func diskFree(dir string) (free, total uint64, err error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, 0, err
	}
	var block uint64
	if st.Bsize > 0 {
		block = uint64(st.Bsize)
	}
	return st.Bavail * block, st.Blocks * block, nil
}
