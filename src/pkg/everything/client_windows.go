//go:build windows

package everything

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

const (
	everythingErrorIPC  = 2
	maxWindowsPathUTF16 = 32768
)

type client struct {
	mu sync.Mutex

	setSearch             *syscall.LazyProc
	setMax                *syscall.LazyProc
	setOffset             *syscall.LazyProc
	query                 *syscall.LazyProc
	getNumResults         *syscall.LazyProc
	getResultFullPathName *syscall.LazyProc
	isFolderResult        *syscall.LazyProc
	getLastError          *syscall.LazyProc
	reset                 *syscall.LazyProc
}

// New loads the Everything SDK DLL from the executable directory.
// Everything itself must also be running so the DLL can query it over IPC.
func New() (Searcher, error) {
	var dllName string
	switch runtime.GOARCH {
	case "amd64":
		dllName = "Everything64.dll"
	case "386":
		dllName = "Everything32.dll"
	case "arm64":
		dllName = "EverythingARM64.dll"
	case "arm":
		dllName = "EverythingARM.dll"
	default:
		return nil, fmt.Errorf("%w: unsupported Windows architecture %s", ErrUnavailable, runtime.GOARCH)
	}

	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("%w: cannot locate wrap.exe: %v", ErrUnavailable, err)
	}
	dllPath := filepath.Join(filepath.Dir(executable), dllName)
	if _, err = os.Stat(dllPath); err != nil {
		return nil, fmt.Errorf("%w: %s was not found beside wrap.exe", ErrUnavailable, dllName)
	}

	dll := syscall.NewLazyDLL(dllPath)
	if err = dll.Load(); err != nil {
		return nil, fmt.Errorf("%w: could not load %s: %v", ErrUnavailable, dllName, err)
	}

	c := &client{
		setSearch:             dll.NewProc("Everything_SetSearchW"),
		setMax:                dll.NewProc("Everything_SetMax"),
		setOffset:             dll.NewProc("Everything_SetOffset"),
		query:                 dll.NewProc("Everything_QueryW"),
		getNumResults:         dll.NewProc("Everything_GetNumResults"),
		getResultFullPathName: dll.NewProc("Everything_GetResultFullPathNameW"),
		isFolderResult:        dll.NewProc("Everything_IsFolderResult"),
		getLastError:          dll.NewProc("Everything_GetLastError"),
		reset:                 dll.NewProc("Everything_Reset"),
	}

	for name, proc := range map[string]*syscall.LazyProc{
		"Everything_SetSearchW":             c.setSearch,
		"Everything_SetMax":                 c.setMax,
		"Everything_SetOffset":              c.setOffset,
		"Everything_QueryW":                 c.query,
		"Everything_GetNumResults":          c.getNumResults,
		"Everything_GetResultFullPathNameW": c.getResultFullPathName,
		"Everything_IsFolderResult":         c.isFolderResult,
		"Everything_GetLastError":           c.getLastError,
		"Everything_Reset":                  c.reset,
	} {
		if err := proc.Find(); err != nil {
			return nil, fmt.Errorf("%w: %s is missing from %s", ErrUnavailable, name, dllName)
		}
	}

	return c, nil
}

func (c *client) Search(query string, maxResults int) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if maxResults <= 0 {
		maxResults = DefaultMaxResults
	}

	search, err := syscall.UTF16PtrFromString(query)
	if err != nil {
		return nil, fmt.Errorf("invalid Everything query: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	defer c.reset.Call()

	c.setSearch.Call(uintptr(unsafe.Pointer(search)))
	c.setOffset.Call(0)
	c.setMax.Call(uintptr(maxResults))
	ok, _, _ := c.query.Call(1)
	runtime.KeepAlive(search)
	if ok == 0 {
		code, _, _ := c.getLastError.Call()
		if code == everythingErrorIPC {
			return nil, fmt.Errorf("%w: Everything is not running", ErrUnavailable)
		}
		return nil, fmt.Errorf("Everything query failed (SDK error %d)", code)
	}

	count, _, _ := c.getNumResults.Call()
	results := make([]Result, 0, int(count))
	buffer := make([]uint16, maxWindowsPathUTF16)
	for i := uintptr(0); i < count; i++ {
		length, _, _ := c.getResultFullPathName.Call(
			i,
			uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(len(buffer)),
		)
		if length == 0 {
			continue
		}
		folder, _, _ := c.isFolderResult.Call(i)
		results = append(results, Result{
			Path:  syscall.UTF16ToString(buffer[:min(int(length), len(buffer))]),
			IsDir: folder != 0,
		})
	}

	return results, nil
}

func IsUnavailable(err error) bool {
	return errors.Is(err, ErrUnavailable)
}
