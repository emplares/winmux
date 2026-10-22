// Testes do Daemon: dispatch de mensagens IPC com sessões mockadas.
// Sem build tags — roda em Linux (sem ConPTY real).
package daemon

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emplares/winmux/internal/ipc"
)

// ---------- mockSession ----------

// mockSession implementa ISession para testes sem ConPTY.
type mockSession struct {
	mu        sync.Mutex // jobs leem a sessão de outra goroutine
	prompt    bool       // PromptVisible
	echo      bool       // Send acrescenta "out:<input>" às linhas e mostra o prompt
	id        string
	pid       int
	shell     string
	dir       string
	createdAt time.Time
	status    string
	state     string
	detail    string
	lines     []string
	logs      []string
	sendErr   error
	resetErr  error
	keyErr    error
	closed    bool
	sent      []string
	submitted []string
	keys      []string
}

func newMockSession(id, shell string) *mockSession {
	return &mockSession{
		id:        id,
		pid:       1000,
		shell:     shell,
		dir:       `C:\`,
		createdAt: time.Now(),
		status:    "active",
		state:     "idle",
	}
}

func (m *mockSession) GetID() string           { return m.id }
func (m *mockSession) GetPID() int             { return m.pid }
func (m *mockSession) GetShell() string        { return m.shell }
func (m *mockSession) GetDir() string          { return m.dir }
func (m *mockSession) GetCreatedAt() time.Time { return m.createdAt }
func (m *mockSession) GetStatus() string       { return m.status }
func (m *mockSession) DetectState() (string, string) {
	return m.state, m.detail
}
func (m *mockSession) ReadSince(cursor int) ([]string, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cursor >= len(m.lines) {
		return nil, len(m.lines)
	}
	return append([]string(nil), m.lines[cursor:]...), len(m.lines)
}
func (m *mockSession) PromptVisible() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.prompt
}
func (m *mockSession) ReadBuffer(n int) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n <= 0 || n > len(m.lines) {
		return m.lines
	}
	return m.lines[len(m.lines)-n:]
}
func (m *mockSession) Logs() []string {
	if m.logs != nil {
		return m.logs
	}
	return m.lines
}
func (m *mockSession) Send(input string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, input)
	if m.echo && m.sendErr == nil {
		m.lines = append(m.lines, "out:"+input)
		m.prompt = true
	}
	return m.sendErr
}
func (m *mockSession) Submit(input string) error {
	m.submitted = append(m.submitted, input)
	return m.sendErr
}
func (m *mockSession) Reset() error { return m.resetErr }
func (m *mockSession) SendKey(key string) error {
	m.keys = append(m.keys, key)
	return m.keyErr
}
func (m *mockSession) Close() { m.closed = true }

// mockFactory retorna uma factory que cria mockSessions rastreáveis.
func mockFactory(sessions map[string]*mockSession) SessionFactory {
	var mu sync.Mutex // a factory é chamada em paralelo por TestDaemon_ConcurrentNew
	return func(id, shell string) (ISession, error) {
		s := newMockSession(id, shell)
		mu.Lock()
		sessions[id] = s
		mu.Unlock()
		return s, nil
	}
}

// failFactory retorna uma factory que sempre falha.
func failFactory(msg string) SessionFactory {
	return func(id, shell string) (ISession, error) {
		return nil, errors.New(msg)
	}
}

// ---------- helpers ----------

func mustHandle[T any](t *testing.T, d *Daemon, req ipc.Request) *T {
	t.Helper()
	raw, err := d.Handle(req)
	if err != nil {
		t.Fatalf("Handle(%q) erro inesperado: %v", req.Action, err)
	}
	resp, ok := raw.(*T)
	if !ok {
		t.Fatalf("Handle(%q): tipo inesperado %T", req.Action, raw)
	}
	return resp
}

// ---------- action "list" ----------

func TestDaemon_ListEmpty(t *testing.T) {
	d := NewWithFactory(mockFactory(map[string]*mockSession{}))

	resp := mustHandle[ipc.ResponseList](t, d, ipc.Request{Action: "list"})
	if resp.Error != "" {
		t.Errorf("list vazio: erro inesperado %q", resp.Error)
	}
	if len(resp.Sessions) != 0 {
		t.Errorf("esperava 0 sessões, obteve %d", len(resp.Sessions))
	}
}

func TestDaemon_ListWithSessions(t *testing.T) {
	created := map[string]*mockSession{}
	d := NewWithFactory(mockFactory(created))

	// Cria duas sessões
	mustHandle[ipc.ResponseNew](t, d, ipc.Request{Action: "new", Shell: "powershell"})
	mustHandle[ipc.ResponseNew](t, d, ipc.Request{Action: "new", Shell: "cmd"})

	resp := mustHandle[ipc.ResponseList](t, d, ipc.Request{Action: "list"})
	if len(resp.Sessions) != 2 {
		t.Errorf("esperava 2 sessões, obteve %d", len(resp.Sessions))
	}
}

func TestDaemon_ListSkipsDeadSessions(t *testing.T) {
	created := map[string]*mockSession{}
	d := NewWithFactory(mockFactory(created))

	// Cria uma sessão
	respNew := mustHandle[ipc.ResponseNew](t, d, ipc.Request{Action: "new", Shell: "powershell"})
	id := respNew.SessionID

	// Marca como morta sem passar por kill
	created[id].status = "dead"

	resp := mustHandle[ipc.ResponseList](t, d, ipc.Request{Action: "list"})
	if len(resp.Sessions) != 0 {
		t.Errorf("sessão morta não deveria aparecer na list, obteve %d", len(resp.Sessions))
	}
}

// ---------- action "new" ----------

func TestDaemon_NewCreatesSession(t *testing.T) {
	created := map[string]*mockSession{}
	d := NewWithFactory(mockFactory(created))

	resp := mustHandle[ipc.ResponseNew](t, d, ipc.Request{Action: "new", Shell: "powershell"})

	if resp.Error != "" {
		t.Errorf("new: erro inesperado %q", resp.Error)
	}
	if resp.SessionID == "" {
		t.Error("new: SessionID deve ser não-vazio")
	}
	if resp.PID != 1000 {
		t.Errorf("new: PID esperado 1000, obteve %d", resp.PID)
	}
	if _, ok := created[resp.SessionID]; !ok {
		t.Error("new: sessão não encontrada no mapa de criadas")
	}
}

func TestDaemon_NewDefaultShell(t *testing.T) {
	created := map[string]*mockSession{}
	d := NewWithFactory(mockFactory(created))

	// Shell vazio → deve usar "powershell"
	resp := mustHandle[ipc.ResponseNew](t, d, ipc.Request{Action: "new"})
	sess := created[resp.SessionID]
	if sess.shell != "powershell" {
		t.Errorf("shell padrão: esperava 'powershell', obteve %q", sess.shell)
	}
}

func TestDaemon_NewUniqueIDs(t *testing.T) {
	d := NewWithFactory(mockFactory(map[string]*mockSession{}))

	ids := map[string]bool{}
	for i := 0; i < 20; i++ {
		resp := mustHandle[ipc.ResponseNew](t, d, ipc.Request{Action: "new"})
		if ids[resp.SessionID] {
			t.Fatalf("ID duplicado gerado: %q (iteração %d)", resp.SessionID, i)
		}
		ids[resp.SessionID] = true
	}
}

func TestDaemon_NewFactoryError(t *testing.T) {
	d := NewWithFactory(failFactory("falha ao criar ConPTY"))

	_, err := d.Handle(ipc.Request{Action: "new"})
	if err == nil {
		t.Error("new com factory com erro deveria retornar erro")
	}
}

// ---------- action "kill" ----------

func TestDaemon_Kill(t *testing.T) {
	created := map[string]*mockSession{}
	d := NewWithFactory(mockFactory(created))

	respNew := mustHandle[ipc.ResponseNew](t, d, ipc.Request{Action: "new"})
	id := respNew.SessionID

	resp := mustHandle[ipc.ResponseKill](t, d, ipc.Request{Action: "kill", ID: id})
	if !resp.OK {
		t.Errorf("kill: esperava OK=true, obteve false (erro: %q)", resp.Error)
	}
	if !created[id].closed {
		t.Error("kill: Close() não foi chamado na sessão")
	}

	// Após kill, a sessão não deve mais aparecer na list
	listResp := mustHandle[ipc.ResponseList](t, d, ipc.Request{Action: "list"})
	for _, s := range listResp.Sessions {
		if s.ID == id {
			t.Error("kill: sessão ainda aparece na list após kill")
		}
	}
}

func TestDaemon_KillNotFound(t *testing.T) {
	d := NewWithFactory(mockFactory(map[string]*mockSession{}))

	resp := mustHandle[ipc.ResponseKill](t, d, ipc.Request{Action: "kill", ID: "naoexiste"})
	if resp.OK {
		t.Error("kill de ID inexistente deveria ter OK=false")
	}
	if resp.Error == "" {
		t.Error("kill de ID inexistente deveria ter mensagem de erro")
	}
}

// ---------- action "send" ----------

func TestDaemon_Send(t *testing.T) {
	created := map[string]*mockSession{}
	d := NewWithFactory(mockFactory(created))

	respNew := mustHandle[ipc.ResponseNew](t, d, ipc.Request{Action: "new"})
	id := respNew.SessionID

	resp := mustHandle[ipc.ResponseSend](t, d, ipc.Request{Action: "send", ID: id, Input: "dir"})
	if !resp.OK {
		t.Errorf("send: esperava OK=true (erro: %q)", resp.Error)
	}
	if len(created[id].sent) != 1 || created[id].sent[0] != "dir" {
		t.Errorf("send: input não chegou na sessão: %v", created[id].sent)
	}
}

func TestDaemon_SendNotFound(t *testing.T) {
	d := NewWithFactory(mockFactory(map[string]*mockSession{}))

	resp := mustHandle[ipc.ResponseSend](t, d, ipc.Request{Action: "send", ID: "xxx", Input: "test"})
	if resp.OK || resp.Error == "" {
		t.Error("send para ID inexistente deveria retornar erro")
	}
}

func TestDaemon_SendError(t *testing.T) {
	created := map[string]*mockSession{}
	d := NewWithFactory(mockFactory(created))

	respNew := mustHandle[ipc.ResponseNew](t, d, ipc.Request{Action: "new"})
	id := respNew.SessionID
	created[id].sendErr = errors.New("pipe quebrado")

	resp := mustHandle[ipc.ResponseSend](t, d, ipc.Request{Action: "send", ID: id, Input: "cmd"})
	if resp.OK || resp.Error == "" {
		t.Error("send com erro na sessão deveria retornar OK=false com mensagem")
	}
}

// ---------- action "submit" ----------

func TestDaemon_Submit(t *testing.T) {
	created := map[string]*mockSession{}
	d := NewWithFactory(mockFactory(created))

	respNew := mustHandle[ipc.ResponseNew](t, d, ipc.Request{Action: "new"})
	id := respNew.SessionID

	resp := mustHandle[ipc.ResponseSend](t, d, ipc.Request{Action: "submit", ID: id, Input: "yes"})
	if !resp.OK {
		t.Errorf("submit: esperava OK=true (erro: %q)", resp.Error)
	}
	if len(created[id].submitted) != 1 || created[id].submitted[0] != "yes" {
		t.Errorf("submit: input não chegou: %v", created[id].submitted)
	}
}

// ---------- action "read" ----------

func TestDaemon_Read(t *testing.T) {
	created := map[string]*mockSession{}
	d := NewWithFactory(mockFactory(created))

	respNew := mustHandle[ipc.ResponseNew](t, d, ipc.Request{Action: "new"})
	id := respNew.SessionID
	created[id].lines = []string{"linha1", "linha2", `PS C:\>`}

	resp := mustHandle[ipc.ResponseRead](t, d, ipc.Request{Action: "read", ID: id, Lines: 10})
	if len(resp.Lines) != 3 {
		t.Errorf("read: esperava 3 linhas, obteve %d", len(resp.Lines))
	}
}

func TestDaemon_ReadDefaultLines(t *testing.T) {
	created := map[string]*mockSession{}
	d := NewWithFactory(mockFactory(created))

	respNew := mustHandle[ipc.ResponseNew](t, d, ipc.Request{Action: "new"})
	id := respNew.SessionID
	// Lines=0 → deve usar 50 como padrão (o mock retorna tudo se n>len)
	created[id].lines = []string{"a", "b"}

	resp := mustHandle[ipc.ResponseRead](t, d, ipc.Request{Action: "read", ID: id})
	if len(resp.Lines) != 2 {
		t.Errorf("read com Lines=0: esperava 2 linhas, obteve %d", len(resp.Lines))
	}
}

// ---------- action "status" ----------

func TestDaemon_StatusIdle(t *testing.T) {
	created := map[string]*mockSession{}
	d := NewWithFactory(mockFactory(created))

	respNew := mustHandle[ipc.ResponseNew](t, d, ipc.Request{Action: "new"})
	id := respNew.SessionID
	created[id].state = "idle"

	resp := mustHandle[ipc.ResponseStatus](t, d, ipc.Request{Action: "status", ID: id})
	if resp.State != "idle" {
		t.Errorf("status: esperava 'idle', obteve %q", resp.State)
	}
}

func TestDaemon_StatusBusy(t *testing.T) {
	created := map[string]*mockSession{}
	d := NewWithFactory(mockFactory(created))

	respNew := mustHandle[ipc.ResponseNew](t, d, ipc.Request{Action: "new"})
	id := respNew.SessionID
	created[id].state = "busy"
	created[id].detail = "5m 30s remaining"

	resp := mustHandle[ipc.ResponseStatus](t, d, ipc.Request{Action: "status", ID: id})
	if resp.State != "busy" {
		t.Errorf("status: esperava 'busy', obteve %q", resp.State)
	}
	if resp.Detail != "5m 30s remaining" {
		t.Errorf("status: detail errado: %q", resp.Detail)
	}
}

func TestDaemon_StatusNotFound(t *testing.T) {
	d := NewWithFactory(mockFactory(map[string]*mockSession{}))

	resp := mustHandle[ipc.ResponseStatus](t, d, ipc.Request{Action: "status", ID: "ghost"})
	if resp.Error == "" {
		t.Error("status para ID inexistente deveria retornar erro")
	}
}

// ---------- action "reset" ----------

func TestDaemon_Reset(t *testing.T) {
	created := map[string]*mockSession{}
	d := NewWithFactory(mockFactory(created))

	respNew := mustHandle[ipc.ResponseNew](t, d, ipc.Request{Action: "new"})
	id := respNew.SessionID

	resp := mustHandle[ipc.ResponseSend](t, d, ipc.Request{Action: "reset", ID: id})
	if !resp.OK {
		t.Errorf("reset: esperava OK=true (erro: %q)", resp.Error)
	}
}

func TestDaemon_ResetError(t *testing.T) {
	created := map[string]*mockSession{}
	d := NewWithFactory(mockFactory(created))

	respNew := mustHandle[ipc.ResponseNew](t, d, ipc.Request{Action: "new"})
	id := respNew.SessionID
	created[id].resetErr = errors.New("erro ctrl+c")

	resp := mustHandle[ipc.ResponseSend](t, d, ipc.Request{Action: "reset", ID: id})
	if resp.OK || resp.Error == "" {
		t.Error("reset com erro deveria retornar OK=false")
	}
}

// ---------- action "key" ----------

func TestDaemon_Key(t *testing.T) {
	created := map[string]*mockSession{}
	d := NewWithFactory(mockFactory(created))

	respNew := mustHandle[ipc.ResponseNew](t, d, ipc.Request{Action: "new"})
	id := respNew.SessionID

	resp := mustHandle[ipc.ResponseKey](t, d, ipc.Request{Action: "key", ID: id, Key: "ctrl+c"})
	if !resp.OK {
		t.Errorf("key: esperava OK=true (erro: %q)", resp.Error)
	}
	if len(created[id].keys) != 1 || created[id].keys[0] != "ctrl+c" {
		t.Errorf("key: tecla não registrada: %v", created[id].keys)
	}
}

func TestDaemon_KeyError(t *testing.T) {
	created := map[string]*mockSession{}
	d := NewWithFactory(mockFactory(created))

	respNew := mustHandle[ipc.ResponseNew](t, d, ipc.Request{Action: "new"})
	id := respNew.SessionID
	created[id].keyErr = errors.New("tecla inválida")

	resp := mustHandle[ipc.ResponseKey](t, d, ipc.Request{Action: "key", ID: id, Key: "xyz"})
	if resp.OK || resp.Error == "" {
		t.Error("key com erro deveria retornar OK=false")
	}
}

// ---------- action "logs" ----------

func TestDaemon_Logs(t *testing.T) {
	created := map[string]*mockSession{}
	d := NewWithFactory(mockFactory(created))

	respNew := mustHandle[ipc.ResponseNew](t, d, ipc.Request{Action: "new"})
	id := respNew.SessionID
	created[id].logs = []string{"[INFO] start", "[WARN] slow", "[INFO] done"}

	resp := mustHandle[ipc.ResponseLogs](t, d, ipc.Request{Action: "logs", ID: id})
	if len(resp.Lines) != 3 {
		t.Errorf("logs: esperava 3 linhas, obteve %d", len(resp.Lines))
	}
	if resp.Lines[0] != "[INFO] start" {
		t.Errorf("logs: primeira linha errada: %q", resp.Lines[0])
	}
}

func TestDaemon_LogsNotFound(t *testing.T) {
	d := NewWithFactory(mockFactory(map[string]*mockSession{}))

	resp := mustHandle[ipc.ResponseLogs](t, d, ipc.Request{Action: "logs", ID: "ghost"})
	if resp.Error == "" {
		t.Error("logs para ID inexistente deveria retornar erro")
	}
}

// ---------- ação desconhecida ----------

func TestDaemon_UnknownAction(t *testing.T) {
	d := NewWithFactory(mockFactory(map[string]*mockSession{}))

	_, err := d.Handle(ipc.Request{Action: "foobar"})
	if err == nil {
		t.Error("ação desconhecida deveria retornar erro")
	}
}

// ---------- fluxo completo ----------

func TestDaemon_FullLifecycle(t *testing.T) {
	created := map[string]*mockSession{}
	d := NewWithFactory(mockFactory(created))

	// 1. Cria sessão
	respNew := mustHandle[ipc.ResponseNew](t, d, ipc.Request{Action: "new", Shell: "powershell"})
	id := respNew.SessionID
	if id == "" {
		t.Fatal("lifecycle: ID vazio")
	}

	// 2. Lista — deve aparecer
	listResp := mustHandle[ipc.ResponseList](t, d, ipc.Request{Action: "list"})
	found := false
	for _, s := range listResp.Sessions {
		if s.ID == id {
			found = true
			if s.Shell != "powershell" {
				t.Errorf("lifecycle: shell errado: %q", s.Shell)
			}
		}
	}
	if !found {
		t.Error("lifecycle: sessão não encontrada na list após new")
	}

	// 3. Envia comando
	created[id].lines = []string{`PS C:\>`}
	sendResp := mustHandle[ipc.ResponseSend](t, d, ipc.Request{Action: "send", ID: id, Input: "Get-Date"})
	if !sendResp.OK {
		t.Errorf("lifecycle: send falhou: %q", sendResp.Error)
	}

	// 4. Lê buffer
	readResp := mustHandle[ipc.ResponseRead](t, d, ipc.Request{Action: "read", ID: id, Lines: 10})
	if len(readResp.Lines) == 0 {
		t.Error("lifecycle: read retornou buffer vazio")
	}

	// 5. Consulta status
	created[id].state = "idle"
	statusResp := mustHandle[ipc.ResponseStatus](t, d, ipc.Request{Action: "status", ID: id})
	if statusResp.State != "idle" {
		t.Errorf("lifecycle: status esperado 'idle', obteve %q", statusResp.State)
	}

	// 6. Kill
	killResp := mustHandle[ipc.ResponseKill](t, d, ipc.Request{Action: "kill", ID: id})
	if !killResp.OK {
		t.Errorf("lifecycle: kill falhou: %q", killResp.Error)
	}
	if !created[id].closed {
		t.Error("lifecycle: sessão não foi fechada após kill")
	}

	// 7. List — não deve mais aparecer
	listFinal := mustHandle[ipc.ResponseList](t, d, ipc.Request{Action: "list"})
	for _, s := range listFinal.Sessions {
		if s.ID == id {
			t.Error("lifecycle: sessão ainda aparece após kill")
		}
	}
}

// ---------- concorrência ----------

func TestDaemon_ConcurrentNew(t *testing.T) {
	d := NewWithFactory(mockFactory(map[string]*mockSession{}))

	done := make(chan string, 50)
	for i := 0; i < 50; i++ {
		go func() {
			resp, err := d.Handle(ipc.Request{Action: "new"})
			if err != nil {
				done <- ""
				return
			}
			done <- resp.(*ipc.ResponseNew).SessionID
		}()
	}

	ids := map[string]bool{}
	for i := 0; i < 50; i++ {
		id := <-done
		if id == "" {
			t.Error("goroutine falhou ao criar sessão")
			continue
		}
		if ids[id] {
			t.Errorf("ID duplicado em concurrent new: %q", id)
		}
		ids[id] = true
	}
}

func TestDaemon_ConcurrentListAndNew(t *testing.T) {
	d := NewWithFactory(mockFactory(map[string]*mockSession{}))

	// Cria 10 sessões
	for i := 0; i < 10; i++ {
		d.Handle(ipc.Request{Action: "new"})
	}

	// Muitas goroutines lendo em paralelo
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func() {
			_, err := d.Handle(ipc.Request{Action: "list"})
			errs <- err
		}()
	}
	for i := 0; i < 20; i++ {
		if err := <-errs; err != nil {
			t.Errorf("list concorrente falhou: %v", err)
		}
	}
}

// ---------- SessionInfo fields ----------

func TestDaemon_NewSessionInfoFields(t *testing.T) {
	created := map[string]*mockSession{}
	d := NewWithFactory(mockFactory(created))

	before := time.Now()
	resp := mustHandle[ipc.ResponseNew](t, d, ipc.Request{Action: "new", Shell: "cmd"})
	after := time.Now()

	listResp := mustHandle[ipc.ResponseList](t, d, ipc.Request{Action: "list"})
	if len(listResp.Sessions) != 1 {
		t.Fatalf("esperava 1 sessão, obteve %d", len(listResp.Sessions))
	}
	s := listResp.Sessions[0]

	if s.ID != resp.SessionID {
		t.Errorf("ID: esperava %q, obteve %q", resp.SessionID, s.ID)
	}
	if s.Shell != "cmd" {
		t.Errorf("Shell: esperava 'cmd', obteve %q", s.Shell)
	}
	if s.Status != "active" {
		t.Errorf("Status: esperava 'active', obteve %q", s.Status)
	}
	if s.CreatedAt.Before(before) || s.CreatedAt.After(after) {
		t.Errorf("CreatedAt fora do intervalo esperado: %v", s.CreatedAt)
	}
}

// Garante que o mock satisfaz a interface ISession em tempo de compilação.
var _ ISession = (*mockSession)(nil)

// Evita "imported and not used" se nenhum teste usar fmt diretamente.
var _ = fmt.Sprintf

// ---------- jobs ----------

func TestJob_CollectsOnlyNewOutputUntilPrompt(t *testing.T) {
	created := map[string]*mockSession{}
	d := NewWithFactory(mockFactory(created))
	id := mustHandle[ipc.ResponseNew](t, d, ipc.Request{Action: "new"}).SessionID
	m := created[id]
	m.lines = []string{"histórico antigo"}
	m.echo = true

	run := mustHandle[ipc.ResponseJobRun](t, d, ipc.Request{Action: "job.run", ID: id, Input: "dir"})
	wait := mustHandle[ipc.ResponseJobStatus](t, d, ipc.Request{Action: "job.wait", ID: run.JobID, Timeout: 10})
	if wait.Job.State != "done" {
		t.Fatalf("job: esperava done, obteve %q (%s)", wait.Job.State, wait.Job.Error)
	}
	out := mustHandle[ipc.ResponseJobOutput](t, d, ipc.Request{Action: "job.output", ID: run.JobID})
	if got := strings.Join(out.Lines, "|"); got != "out:dir" {
		t.Errorf("job output: esperava só a linha nova, obteve %q", got)
	}
}

func TestJob_NotDoneWithoutPrompt(t *testing.T) {
	created := map[string]*mockSession{}
	d := NewWithFactory(mockFactory(created))
	id := mustHandle[ipc.ResponseNew](t, d, ipc.Request{Action: "new"}).SessionID

	// idle mas sem prompt (ex: Start-Sleep em andamento) => continua running
	run := mustHandle[ipc.ResponseJobRun](t, d, ipc.Request{Action: "job.run", ID: id, Input: "Start-Sleep 60"})
	wait := mustHandle[ipc.ResponseJobStatus](t, d, ipc.Request{Action: "job.wait", ID: run.JobID, Timeout: 3})
	if wait.Job.State != "running" {
		t.Fatalf("job: esperava running, obteve %q", wait.Job.State)
	}
	kill := mustHandle[ipc.ResponseJobKill](t, d, ipc.Request{Action: "job.kill", ID: run.JobID})
	if !kill.OK {
		t.Fatalf("job.kill falhou: %s", kill.Error)
	}
	mustHandle[ipc.ResponseJobKill](t, d, ipc.Request{Action: "job.kill", ID: run.JobID}) // segundo kill não pode dar panic
}
