package server

import "golang.org/x/sys/windows"

// diskFree returns the bytes free for the user and the size of the disk of dir.
func diskFree(dir string) (free, total uint64, err error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, 0, err
	}
	if err := windows.GetDiskFreeSpaceEx(p, &free, &total, nil); err != nil {
		return 0, 0, err
	}
	return free, total, nil
}
