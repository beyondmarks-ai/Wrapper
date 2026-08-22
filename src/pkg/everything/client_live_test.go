//go:build windows && everythinglive

package everything

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestLiveSearch requires Everything to be running and the matching SDK DLL
// beside the compiled test executable. It is intentionally excluded from the
// normal test suite so CI does not require a desktop application.
func TestLiveSearch(t *testing.T) {
	client, err := New()
	if err != nil {
		t.Fatalf("load Everything SDK: %v", err)
	}

	results, err := client.Search("Everything.exe", DefaultMaxResults)
	if err != nil {
		t.Fatalf("query Everything index: %v", err)
	}
	for _, result := range results {
		if strings.EqualFold(filepath.Base(result.Path), "Everything.exe") {
			t.Logf("live Everything result: %s", result.Path)
			return
		}
	}

	t.Fatalf("Everything.exe not found in %d indexed results", len(results))
}
