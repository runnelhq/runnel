//go:build !windows

package excel

// locked reports that a file cannot be replaced because another program has it open;
// only Windows locks open files.
func locked(error) bool { return false }
