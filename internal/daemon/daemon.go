//go:build windows

// daemon.go — partes Windows-específicas do Daemon: HandleAttach e handleWatch.
package daemon

import (
	"fmt"
	"net"
	"os/exec"
	"time"

	"github.com/emplares/winmux/internal/ipc"
)

// HandleAttach processa um attach — protocolo muda para streaming raw após OK.
func (d *Daemon) HandleAttach(id string, conn net.Conn) {
	d.mu.RLock()
	sess, ok := d.sessions[id]
	d.mu.RUnlock()

	if !ok {
		resp := ipc.ResponseAttach{Error: fmt.Sprintf("sessão %q não encontrada", id)}
		writeJSON(conn, resp)
		return
	}

	// Confirma attach
	writeJSON(conn, ipc.ResponseAttach{OK: true, SessionID: id})

	// Streaming bidirecional — requer net.Conn + cpty (Windows)
	// Faz type assertion para Session concreta
	concrete, ok := sess.(*Session)
	if !ok {
		writeJSON(conn, ipc.ResponseAttach{Error: "sessão não é do tipo Session"})
		return
	}
	done := make(chan struct{})
	concrete.Attach(conn, conn, done)
	<-done
}

// handleWatch inicia watch em goroutine separada e retorna confirmação imediata.
func (d *Daemon) handleWatch(req ipc.Request) (*ipc.ResponseWatch, error) {
	d.mu.RLock()
	_, ok := d.sessions[req.ID]
	d.mu.RUnlock()
	if !ok {
		return &ipc.ResponseWatch{Error: fmt.Sprintf("sessão %q não encontrada", req.ID)}, nil
	}

	go d.runWatch(req)
	return &ipc.ResponseWatch{OK: true}, nil
}

// runWatch fica em polling e executa callbacks quando o estado muda.
func (d *Daemon) runWatch(req ipc.Request) {
	timeout := time.Duration(req.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	deadline := time.Now().Add(timeout)

	var lastState string
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		if time.Now().After(deadline) {
			return
		}

		<-ticker.C

		d.mu.RLock()
		sess, ok := d.sessions[req.ID]
		d.mu.RUnlock()
		if !ok {
			return
		}

		state, _ := sess.DetectState()

		if state == lastState {
			continue
		}
		lastState = state

		var cb string
		switch state {
		case "idle":
			cb = req.OnIdle
		case "waiting":
			cb = req.OnWait
		}

		if cb != "" {
			exec.Command("cmd", "/C", cb).Start()
		}

		if state == "idle" && req.OnIdle != "" {
			return
		}
		if state == "waiting" && req.OnWait != "" && req.OnIdle == "" {
			return
		}
	}
}
