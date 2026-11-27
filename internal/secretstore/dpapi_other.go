//go:build !windows

package secretstore

import "errors"

// Without DPAPI the key comes only from RUNNEL_MASTER_KEY.
const dpapiAvailable = false

var errNoDPAPI = errors.New("DPAPI is only available on Windows")

func protect([]byte, bool) ([]byte, error) { return nil, errNoDPAPI }

func unprotect([]byte, bool) ([]byte, error) { return nil, errNoDPAPI }
