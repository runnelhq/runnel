package secretstore

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const dpapiAvailable = true

// entropy is passed to DPAPI as well, so that other programs of the account cannot
// decrypt the key by just calling DPAPI with the file.
var entropy = []byte("runnel secret store v1")

// protect encrypts data with DPAPI: for the user, or with machine for every account of
// this computer (the store of the service; its folder is protected by permissions).
func protect(data []byte, machine bool) ([]byte, error) {
	var out windows.DataBlob
	if err := windows.CryptProtectData(blob(data), nil, blob(entropy), 0, nil, flags(machine), &out); err != nil {
		return nil, err
	}
	return take(&out), nil
}

// unprotect decrypts data protected by protect.
func unprotect(data []byte, machine bool) ([]byte, error) {
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(blob(data), nil, blob(entropy), 0, nil, flags(machine), &out); err != nil {
		return nil, err
	}
	return take(&out), nil
}

func flags(machine bool) uint32 {
	f := uint32(windows.CRYPTPROTECT_UI_FORBIDDEN)
	if machine {
		f |= windows.CRYPTPROTECT_LOCAL_MACHINE
	}
	return f
}

func blob(b []byte) *windows.DataBlob {
	if len(b) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(b)), Data: &b[0]} //nolint:gosec // keys and blobs are small
}

// take copies the output of DPAPI and frees it.
func take(b *windows.DataBlob) []byte {
	out := append([]byte(nil), unsafe.Slice(b.Data, b.Size)...) //nolint:gosec // b.Size bytes returned by DPAPI

	_, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(b.Data))) //nolint:gosec // memory of DPAPI, freed as documented
	return out
}
