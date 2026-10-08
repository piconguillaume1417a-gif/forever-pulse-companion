//go:build !windows

package update

import (
	"errors"
	"time"
)

func Relance(exe string, pid int) error { return errors.New("mise à jour : Windows seulement") }

func AttendFin(pid int, d time.Duration) {}
