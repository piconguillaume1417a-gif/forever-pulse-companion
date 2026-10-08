//go:build windows

package update

import (
	"os/exec"
	"strconv"
	"time"

	"golang.org/x/sys/windows"
)

// Relance démarre exe en icône seule. La nouvelle instance attend la fin de pid
// avant de réclamer l'instance unique (le verrou nommé n'est libéré qu'à la sortie).
func Relance(exe string, pid int) error {
	return exec.Command(exe, "--tray", "--after-update", strconv.Itoa(pid)).Start()
}

// AttendFin attend la sortie du processus pid, au plus d.
func AttendFin(pid int, d time.Duration) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return // déjà sorti
	}
	defer windows.CloseHandle(h)
	_, _ = windows.WaitForSingleObject(h, uint32(d/time.Millisecond))
}
