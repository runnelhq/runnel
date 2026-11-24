package excel

import (
	"errors"

	"golang.org/x/sys/windows"
)

// locked reports that a file cannot be replaced because another program has it open,
// as Excel does with an open workbook.
func locked(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
