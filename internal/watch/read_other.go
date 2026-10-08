//go:build !windows

package watch

import "os"

func openShared(path string) (*os.File, error) { return os.Open(path) }
