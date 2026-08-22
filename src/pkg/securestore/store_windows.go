//go:build windows

package securestore

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func protect(plain []byte) ([]byte, error)        { return crypt(plain, false) }
func unprotect(ciphertext []byte) ([]byte, error) { return crypt(ciphertext, true) }

func crypt(input []byte, decrypt bool) ([]byte, error) {
	if len(input) == 0 {
		return nil, nil
	}
	in := windows.DataBlob{Size: uint32(len(input)), Data: &input[0]}
	var out windows.DataBlob
	var err error
	if decrypt {
		err = windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, fmt.Errorf("protect data with Windows DPAPI: %w", err)
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(out.Data)))) //nolint:errcheck
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}
