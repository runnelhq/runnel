//go:build !linux && !darwin && !windows

package server

import "errors"

// diskFree is not available on this system; GET /health says so.
func diskFree(string) (free, total uint64, err error) {
	return 0, 0, errDiskFree
}

var errDiskFree = errors.New("not available on this system")
