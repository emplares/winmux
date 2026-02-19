// daemon_core.go — lógica central do Daemon sem dependências Windows.
// Sem build tags: compila em qualquer GOOS.
package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/emplares/winmux/internal/ipc"
)

// ISession define a interface de uma sessão (implementada por Session no Windows
// e por mocks nos testes Linux).
type ISession interface {
	GetID() string
	GetPID() int
	GetShell() string
	GetDir() string
	GetCreatedAt() time.Time
	GetStatus() string
	DetectState() (state, detail string)
	ReadBuffer(n int) []string
	Logs() []string
	Send(input string) error
	Submit(input string) error
	Reset() error
	SendKey(key string) error
	Close()
}

// SessionFactory é uma função que cria novas sessões (injetável para testes).
type SessionFactory func(id, shell string) (ISession, error)

// Daemon gerencia o conjunto de sessões ativas.
type Daemon struct {
	mu       sync.RWMutex
	sessions map[string]ISession
	factory  SessionFactory
}

// New cria um novo Daemon com a factory padrão (plataforma-específica).
func New() *Daemon {
	return &Daemon{
		sessions: make(map[string]ISession),
		factory:  defaultSessionFactory,
	}
}

// NewWithFactory cria um Daemon com factory customizada (para testes).
func NewWithFactory(factory SessionFactory) *Daemon {
	return &Daemon{
		sessions: make(map[string]ISession),
		factory:  factory,
	}
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
	case "key":
		return d.handleKey(req)
	case "logs":
		return d.handleLogs(req)
	default:
		return nil, fmt.Errorf("ação desconhecida: %s", req.Action)
	}
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
	sess, err := d.factory(id, shell)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.sessions[id] = sess
	d.mu.Unlock()
	return &ipc.ResponseNew{SessionID: sess.GetID(), PID: sess.GetPID()}, nil
}

func (d *Daemon) handleList() (*ipc.ResponseList, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	// Limpa sessões mortas
	for id, s := range d.sessions {
		if s.GetStatus() == "dead" {
			delete(d.sessions, id)
		}
	}
	sessions := []ipc.SessionInfo{}
	for _, s := range d.sessions {
		sessions = append(sessions, ipc.SessionInfo{
			ID:        s.GetID(),
			PID:       s.GetPID(),
			Shell:     s.GetShell(),
			Dir:       s.GetDir(),
			CreatedAt: s.GetCreatedAt(),
			Status:    s.GetStatus(),
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

func (d *Daemon) handleLogs(req ipc.Request) (*ipc.ResponseLogs, error) {
	d.mu.RLock()
	sess, ok := d.sessions[req.ID]
	d.mu.RUnlock()
	if !ok {
		return &ipc.ResponseLogs{Error: fmt.Sprintf("sessão %q não encontrada", req.ID)}, nil
	}
	return &ipc.ResponseLogs{Lines: sess.Logs()}, nil
}

// HandleAttach é implementado somente em daemon.go (Windows) pois usa net.Conn raw.
// No Linux, esta função é um stub.

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
