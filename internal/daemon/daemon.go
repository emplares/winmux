//go:build windows

package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"sync"
	"time"

	"github.com/emplares/winmux/internal/ipc"
)

// Daemon gerencia o conjunto de sessões ativas.
type Daemon struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// New cria um novo Daemon.
func New() *Daemon {
	return &Daemon{sessions: make(map[string]*Session)}
}

// Handle processa requests JSON do CLI.
func (d *Daemon) Handle(req ipc.Request) (any, error) {
	switch req.Action {
	case "new":
		return d.handleNew(req)
	case "list":
		return d.handleList()
	case "kill":
		return d.handleKill(req)
	case "send":
		return d.handleSend(req)
	case "submit":
		return d.handleSubmit(req)
	case "read":
		return d.handleRead(req)
	case "status":
		return d.handleStatus(req)
	case "reset":
		return d.handleReset(req)
	case "watch":
		return d.handleWatch(req)
	case "key":
		return d.handleKey(req)
	default:
		return nil, fmt.Errorf("ação desconhecida: %s", req.Action)
	}
}

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

	// Streaming bidirecional
	done := make(chan struct{})
	sess.Attach(conn, conn, done)
	<-done
}

func (d *Daemon) handleNew(req ipc.Request) (*ipc.ResponseNew, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	shell := req.Shell
	if shell == "" {
		shell = "powershell"
	}
	sess, err := NewSession(id, shell)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.sessions[id] = sess
	d.mu.Unlock()
	return &ipc.ResponseNew{SessionID: id, PID: sess.PID}, nil
}

func (d *Daemon) handleList() (*ipc.ResponseList, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	// Limpa sessões mortas
	for id, s := range d.sessions {
		if s.Status == "dead" {
			delete(d.sessions, id)
		}
	}
	var sessions []ipc.SessionInfo
	for _, s := range d.sessions {
		sessions = append(sessions, ipc.SessionInfo{
			ID:        s.ID,
			PID:       s.PID,
			Shell:     s.Shell,
			Dir:       s.Dir,
			CreatedAt: s.CreatedAt,
			Status:    s.Status,
		})
	}
	return &ipc.ResponseList{Sessions: sessions}, nil
}

func (d *Daemon) handleKill(req ipc.Request) (*ipc.ResponseKill, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	sess, ok := d.sessions[req.ID]
	if !ok {
		return &ipc.ResponseKill{Error: fmt.Sprintf("sessão %q não encontrada", req.ID)}, nil
	}
	sess.Close()
	delete(d.sessions, req.ID)
	return &ipc.ResponseKill{OK: true}, nil
}

func (d *Daemon) handleSend(req ipc.Request) (*ipc.ResponseSend, error) {
	d.mu.RLock()
	sess, ok := d.sessions[req.ID]
	d.mu.RUnlock()
	if !ok {
		return &ipc.ResponseSend{Error: fmt.Sprintf("sessão %q não encontrada", req.ID)}, nil
	}
	if err := sess.Send(req.Input); err != nil {
		return &ipc.ResponseSend{Error: err.Error()}, nil
	}
	return &ipc.ResponseSend{OK: true}, nil
}

// handleSubmit é igual ao send mas com semântica explícita para TUIs interativas.
func (d *Daemon) handleSubmit(req ipc.Request) (*ipc.ResponseSend, error) {
	d.mu.RLock()
	sess, ok := d.sessions[req.ID]
	d.mu.RUnlock()
	if !ok {
		return &ipc.ResponseSend{Error: fmt.Sprintf("sessão %q não encontrada", req.ID)}, nil
	}
	if err := sess.Submit(req.Input); err != nil {
		return &ipc.ResponseSend{Error: err.Error()}, nil
	}
	return &ipc.ResponseSend{OK: true}, nil
}

func (d *Daemon) handleRead(req ipc.Request) (*ipc.ResponseRead, error) {
	d.mu.RLock()
	sess, ok := d.sessions[req.ID]
	d.mu.RUnlock()
	if !ok {
		return &ipc.ResponseRead{Error: fmt.Sprintf("sessão %q não encontrada", req.ID)}, nil
	}
	n := req.Lines
	if n == 0 {
		n = 50
	}
	return &ipc.ResponseRead{Lines: sess.ReadBuffer(n)}, nil
}

func (d *Daemon) handleStatus(req ipc.Request) (*ipc.ResponseStatus, error) {
	d.mu.RLock()
	sess, ok := d.sessions[req.ID]
	d.mu.RUnlock()
	if !ok {
		return &ipc.ResponseStatus{Error: fmt.Sprintf("sessão %q não encontrada", req.ID)}, nil
	}
	state, detail := sess.DetectState()
	return &ipc.ResponseStatus{State: state, Detail: detail}, nil
}

func (d *Daemon) handleReset(req ipc.Request) (*ipc.ResponseSend, error) {
	d.mu.RLock()
	sess, ok := d.sessions[req.ID]
	d.mu.RUnlock()
	if !ok {
		return &ipc.ResponseSend{Error: fmt.Sprintf("sessão %q não encontrada", req.ID)}, nil
	}
	if err := sess.Reset(); err != nil {
		return &ipc.ResponseSend{Error: err.Error()}, nil
	}
	return &ipc.ResponseSend{OK: true}, nil
}

func (d *Daemon) handleKey(req ipc.Request) (*ipc.ResponseKey, error) {
	d.mu.RLock()
	sess, ok := d.sessions[req.ID]
	d.mu.RUnlock()
	if !ok {
		return &ipc.ResponseKey{Error: fmt.Sprintf("sessão %q não encontrada", req.ID)}, nil
	}
	if err := sess.SendKey(req.Key); err != nil {
		return &ipc.ResponseKey{Error: err.Error()}, nil
	}
	return &ipc.ResponseKey{OK: true}, nil
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
		timeout = 30 * time.Minute // timeout padrão de 30 minutos
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
			return // sessão foi encerrada
		}

		state, _ := sess.DetectState()

		if state == lastState {
			continue
		}
		lastState = state

		// Executa callback correspondente
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

		// Se chegou idle e tinha callback, encerra watch
		if state == "idle" && req.OnIdle != "" {
			return
		}
		if state == "waiting" && req.OnWait != "" && req.OnIdle == "" {
			return
		}
	}
}

func newID() (string, error) {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func writeJSON(conn net.Conn, v any) {
	data, _ := json.Marshal(v)
	conn.Write(append(data, '\n'))
}
