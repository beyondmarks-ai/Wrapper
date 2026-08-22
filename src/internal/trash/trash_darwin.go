//go:build darwin && cgo

package trash

/*
#cgo LDFLAGS: -framework Foundation
#include <stdlib.h>

typedef struct {
	char *trashedPath;
	char *errorMessage;
} WRAPTrashResult;

WRAPTrashResult wrap_trash_item(const char *path);
void wrap_free_string(char *value);
*/
import "C"

import (
	"errors"
	"path/filepath"
	"unsafe"
)

func Init() error {
	return nil
}

func Available(path string) bool {
	return path != ""
}

func Move(path string) (Result, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return Result{OriginalPath: path, Backend: BackendMacOS}, err
	}

	cPath := C.CString(absPath)
	defer C.free(unsafe.Pointer(cPath))

	result := C.wrap_trash_item(cPath)
	defer C.wrap_free_string(result.trashedPath)
	defer C.wrap_free_string(result.errorMessage)

	if result.errorMessage != nil {
		return Result{OriginalPath: absPath, Backend: BackendMacOS}, errors.New(C.GoString(result.errorMessage))
	}
	trashedPath := ""
	if result.trashedPath != nil {
		trashedPath = C.GoString(result.trashedPath)
	}
	return Result{
		OriginalPath:     absPath,
		TrashedPath:      trashedPath,
		Backend:          BackendMacOS,
		StrictlyRecycled: true,
	}, nil
}
