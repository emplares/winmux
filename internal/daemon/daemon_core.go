// daemon_core.go — lógica central do Daemon sem dependências Windows.
// Sem build tags: compila em qualquer GOOS.
package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"strings"
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
	ReadSince(cursor int) ([]string, int)
	PromptVisible() bool
	Logs() []string
	Send(input string) error
	Submit(input string) error
	Reset() error
	SendKey(key string) error
	Close()
}

// SessionFactory é uma função que cria novas sessões (injetável para testes).
type SessionFactory func(id, shell string) (ISession, error)

// job representa um job assíncrono disparado por um agente.
type job struct {
	id        string
	sessionID string
	command   string
	state     string // "running" | "done" | "error"
	output    []string
	startedAt time.Time
	endedAt   *time.Time
	errMsg    string
	cancel    func() // cancela a goroutine do job
	mu        sync.RWMutex
}

func (j *job) info() ipc.JobInfo {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return ipc.JobInfo{
		ID:        j.id,
		SessionID: j.sessionID,
		Command:   j.command,
		State:     j.state,
		Lines:     len(j.output),
		StartedAt: j.startedAt,
		EndedAt:   j.endedAt,
		Error:     j.errMsg,
	}
}

// Daemon gerencia o conjunto de sessões ativas.
type Daemon struct {
	mu       sync.RWMutex
	sessions map[string]ISession
	factory  SessionFactory
	jobsMu   sync.RWMutex
	jobs     map[string]*job
}

// New cria um novo Daemon com a factory padrão (plataforma-específica).
func New() *Daemon {
	return &Daemon{
		sessions: make(map[string]ISession),
		factory:  defaultSessionFactory,
		jobs:     make(map[string]*job),
	}
}

// NewWithFactory cria um Daemon com factory customizada (para testes).
func NewWithFactory(factory SessionFactory) *Daemon {
	return &Daemon{
		sessions: make(map[string]ISession),
		factory:  factory,
		jobs:     make(map[string]*job),
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
	case "job.run":
		return d.handleJobRun(req)
	case "job.status":
		return d.handleJobStatus(req)
	case "job.output":
		return d.handleJobOutput(req)
	case "job.list":
		return d.handleJobList()
	case "job.kill":
		return d.handleJobKill(req)
	case "job.wait":
		return d.handleJobWait(req)
	default:
		return nil, fmt.Errorf("unknown action: %s", req.Action)
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
		return &ipc.ResponseKill{Error: fmt.Sprintf("session %q not found", req.ID)}, nil
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
		return &ipc.ResponseSend{Error: fmt.Sprintf("session %q not found", req.ID)}, nil
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
		return &ipc.ResponseSend{Error: fmt.Sprintf("session %q not found", req.ID)}, nil
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
		return &ipc.ResponseRead{Error: fmt.Sprintf("session %q not found", req.ID)}, nil
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
		return &ipc.ResponseStatus{Error: fmt.Sprintf("session %q not found", req.ID)}, nil
	}
	state, detail := sess.DetectState()
	return &ipc.ResponseStatus{State: state, Detail: detail}, nil
}

func (d *Daemon) handleReset(req ipc.Request) (*ipc.ResponseSend, error) {
	d.mu.RLock()
	sess, ok := d.sessions[req.ID]
	d.mu.RUnlock()
	if !ok {
		return &ipc.ResponseSend{Error: fmt.Sprintf("session %q not found", req.ID)}, nil
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
		return &ipc.ResponseKey{Error: fmt.Sprintf("session %q not found", req.ID)}, nil
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
		return &ipc.ResponseLogs{Error: fmt.Sprintf("session %q not found", req.ID)}, nil
	}
	return &ipc.ResponseLogs{Lines: sess.Logs()}, nil
}

// ── Job handlers ──────────────────────────────────────────────────────────────

func (d *Daemon) handleJobRun(req ipc.Request) (*ipc.ResponseJobRun, error) {
	d.mu.RLock()
	sess, ok := d.sessions[req.ID]
	d.mu.RUnlock()
	if !ok {
		return &ipc.ResponseJobRun{Error: fmt.Sprintf("session %q not found", req.ID)}, nil
	}

	jobID, err := newID()
	if err != nil {
		return nil, err
	}

	j := &job{
		id:        jobID,
		sessionID: req.ID,
		command:   req.Input,
		state:     "running",
		output:    []string{},
		startedAt: time.Now(),
	}

	// canal para cancelamento (job.kill pode chegar mais de uma vez)
	done := make(chan struct{})
	j.cancel = sync.OnceFunc(func() { close(done) })

	d.jobsMu.Lock()
	d.jobs[jobID] = j
	d.jobsMu.Unlock()

	finish := func(state, errMsg string) {
		j.mu.Lock()
		j.state = state
		j.errMsg = errMsg
		now := time.Now()
		j.endedAt = &now
		j.mu.Unlock()
	}
	// cursor absoluto: só entra no job o que a sessão imprimir depois do envio.
	// A primeira linha completa é a do prompt com o comando digitado (eco) — fica de fora.
	// ponytail: comando longo que quebra em várias linhas de tela deixa eco extra; contar colunas se incomodar.
	_, cursor := sess.ReadSince(math.MaxInt)
	cursor++
	collect := func() {
		lines, next := sess.ReadSince(cursor)
		cursor = max(cursor, next)
		for i, l := range lines {
			lines[i] = strings.TrimRight(stripAnsi(l), " ") // modo agente: texto limpo, sem cores/cursor
		}
		if len(lines) > 0 {
			j.mu.Lock()
			j.output = append(j.output, lines...)
			j.mu.Unlock()
		}
	}

	go func() {
		if err := sess.Send(req.Input); err != nil {
			finish("error", err.Error())
			return
		}

		// Conclusão = estado idle com o prompt do shell de volta na linha corrente,
		// por 3 ciclos seguidos (1.5s). Sem exigir o prompt, um comando silencioso
		// (Start-Sleep) pareceria concluído.
		// ponytail: heurística de prompt ("...>"); TUIs sem prompt nunca concluem — use status/watch para elas.
		idleCount := 0
		for {
			select {
			case <-done:
				collect()
				finish("error", "cancelled")
				return
			case <-time.After(500 * time.Millisecond):
			}
			collect()
			if state, _ := sess.DetectState(); state == "idle" && sess.PromptVisible() {
				idleCount++
				if idleCount >= 3 {
					collect()
					finish("done", "")
					return
				}
			} else {
				idleCount = 0
			}
		}
	}()

	// agenda limpeza após 1h
	go func() {
		select {
		case <-time.After(1 * time.Hour):
			d.jobsMu.Lock()
			delete(d.jobs, jobID)
			d.jobsMu.Unlock()
		}
	}()

	return &ipc.ResponseJobRun{JobID: jobID}, nil
}

func (d *Daemon) handleJobStatus(req ipc.Request) (*ipc.ResponseJobStatus, error) {
	d.jobsMu.RLock()
	j, ok := d.jobs[req.ID]
	d.jobsMu.RUnlock()
	if !ok {
		return &ipc.ResponseJobStatus{Error: fmt.Sprintf("job %q not found", req.ID)}, nil
	}
	return &ipc.ResponseJobStatus{Job: j.info()}, nil
}

func (d *Daemon) handleJobOutput(req ipc.Request) (*ipc.ResponseJobOutput, error) {
	d.jobsMu.RLock()
	j, ok := d.jobs[req.ID]
	d.jobsMu.RUnlock()
	if !ok {
		return &ipc.ResponseJobOutput{Error: fmt.Sprintf("job %q not found", req.ID)}, nil
	}
	j.mu.RLock()
	lines := make([]string, len(j.output))
	copy(lines, j.output)
	j.mu.RUnlock()

	// --tail N
	if req.Lines > 0 && req.Lines < len(lines) {
		lines = lines[len(lines)-req.Lines:]
	}
	return &ipc.ResponseJobOutput{Lines: lines}, nil
}

func (d *Daemon) handleJobList() (*ipc.ResponseJobList, error) {
	d.jobsMu.RLock()
	defer d.jobsMu.RUnlock()
	jobs := make([]ipc.JobInfo, 0, len(d.jobs))
	for _, j := range d.jobs {
		jobs = append(jobs, j.info())
	}
	return &ipc.ResponseJobList{Jobs: jobs}, nil
}

func (d *Daemon) handleJobKill(req ipc.Request) (*ipc.ResponseJobKill, error) {
	d.jobsMu.Lock()
	j, ok := d.jobs[req.ID]
	d.jobsMu.Unlock()
	if !ok {
		return &ipc.ResponseJobKill{Error: fmt.Sprintf("job %q not found", req.ID)}, nil
	}
	j.mu.RLock()
	state := j.state
	j.mu.RUnlock()
	if state != "running" {
		return &ipc.ResponseJobKill{Error: fmt.Sprintf("job %q is not running (state: %s)", req.ID, state)}, nil
	}
	if j.cancel != nil {
		j.cancel()
	}
	return &ipc.ResponseJobKill{OK: true}, nil
}

func (d *Daemon) handleJobWait(req ipc.Request) (*ipc.ResponseJobStatus, error) {
	d.jobsMu.RLock()
	j, ok := d.jobs[req.ID]
	d.jobsMu.RUnlock()
	if !ok {
		return &ipc.ResponseJobStatus{Error: fmt.Sprintf("job %q not found", req.ID)}, nil
	}

	timeout := time.Duration(req.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		j.mu.RLock()
		state := j.state
		j.mu.RUnlock()
		if state != "running" {
			break
		}
		time.Sleep(1 * time.Second)
	}

	return &ipc.ResponseJobStatus{Job: j.info()}, nil
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
