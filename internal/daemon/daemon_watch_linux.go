//go:build !windows

// daemon_watch_linux.go — stub do handleWatch para sistemas não-Windows.
package daemon

import "github.com/emplares/winmux/internal/ipc"

// handleWatch não faz nada útil no Linux (sem ConPTY real).
func (d *Daemon) handleWatch(req ipc.Request) (*ipc.ResponseWatch, error) {
	return &ipc.ResponseWatch{OK: true}, nil
}
