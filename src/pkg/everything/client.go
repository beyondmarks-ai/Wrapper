// Package everything provides fast, indexed file search through the Everything SDK.
package everything

import "errors"

const DefaultMaxResults = 100

var (
	ErrUnavailable = errors.New("Everything search is unavailable")
	ErrUnsupported = errors.New("Everything search is only supported on Windows")
)

type Result struct {
	Path  string
	IsDir bool
}

// Searcher is implemented by the platform client and kept small so the UI can
// be tested without loading the native Everything SDK.
type Searcher interface {
	Search(query string, maxResults int) ([]Result, error)
}
