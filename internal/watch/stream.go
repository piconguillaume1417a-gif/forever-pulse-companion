package watch

import (
	"errors"
	"os"
)

// OpenSnapshot opens the live file with WoW-compatible sharing and captures
// identity, size and modification time. Check it again before committing.
func OpenSnapshot(path string) (*os.File, func() error, error) {
	f, err := openShared(path)
	if err != nil {
		return nil, nil, err
	}
	initial, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return f, func() error {
		opened, err := f.Stat()
		if err != nil {
			return err
		}
		current, err := os.Stat(path)
		if err != nil {
			return err
		}
		for _, st := range []os.FileInfo{opened, current} {
			if !os.SameFile(initial, st) || initial.Size() != st.Size() || !initial.ModTime().Equal(st.ModTime()) {
				return errors.New("SavedVariables changed while being read")
			}
		}
		return nil
	}, nil
}
