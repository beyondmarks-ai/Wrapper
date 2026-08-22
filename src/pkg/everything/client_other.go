//go:build !windows

package everything

func New() (Searcher, error) {
	return nil, ErrUnsupported
}

func IsUnavailable(err error) bool {
	return err != nil
}
