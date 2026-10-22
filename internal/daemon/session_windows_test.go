//go:build windows

// Testes de integração com ConPTY real. Rodam no CI (windows-latest).
package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/emplares/winmux/internal/ipc"
)

func waitFor(t *testing.T, s *Session, what string, d time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timeout esperando %s; buffer:\n%s\nlinha corrente: %q", what, strings.Join(s.logs(), "\n"), string(s.line))
}

func TestConPTY_PowerShellRoundTrip(t *testing.T) {
	s, err := NewSession("it0001", "powershell")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	waitFor(t, s, "primeiro prompt", 30*time.Second, s.PromptVisible)

	// attach em paralelo não pode roubar output do buffer
	var attached strings.Builder
	unsub := s.subscribe(writerFunc(func(p []byte) (int, error) { attached.Write(p); return len(p), nil }))
	defer unsub()

	// concatenação para não casar com o eco do próprio comando
	if err := s.Send("Write-Output ('winmux' + '-ok')"); err != nil {
		t.Fatal(err)
	}
	hasLine := func() bool {
		for _, l := range s.ReadBuffer(50) {
			if strings.TrimSpace(stripAnsi(l)) == "winmux-ok" {
				return true
			}
		}
		return false
	}
	waitFor(t, s, "linha winmux-ok", 15*time.Second, hasLine)
	waitFor(t, s, "prompt de volta", 15*time.Second, s.PromptVisible)
	if state, detail := s.DetectState(); state != "idle" {
		t.Errorf("esperava idle, obteve %q (%s)", state, detail)
	}
	if !strings.Contains(stripAnsi(attached.String()), "winmux-ok") {
		t.Errorf("attach não recebeu o output")
	}
}

func TestConPTY_JobWaitsForSilentCommand(t *testing.T) {
	d := New()
	resp, err := d.Handle(ipc.Request{Action: "new"})
	if err != nil {
		t.Fatal(err)
	}
	id := resp.(*ipc.ResponseNew).SessionID
	defer d.Handle(ipc.Request{Action: "kill", ID: id})
	s := d.sessions[id].(*Session)
	waitFor(t, s, "primeiro prompt", 30*time.Second, s.PromptVisible)

	start := time.Now()
	run, _ := d.Handle(ipc.Request{Action: "job.run", ID: id, Input: "Start-Sleep 4; Write-Output ('job' + '-done')"})
	jobID := run.(*ipc.ResponseJobRun).JobID
	wait, _ := d.Handle(ipc.Request{Action: "job.wait", ID: jobID, Timeout: 60})
	job := wait.(*ipc.ResponseJobStatus).Job
	if job.State != "done" {
		t.Fatalf("esperava done, obteve %q (%s)", job.State, job.Error)
	}
	if el := time.Since(start); el < 4*time.Second {
		t.Errorf("job concluiu cedo demais (%s): Start-Sleep 4 ainda rodava", el)
	}
	out, _ := d.Handle(ipc.Request{Action: "job.output", ID: jobID})
	if !strings.Contains(strings.Join(out.(*ipc.ResponseJobOutput).Lines, "\n"), "job-done") {
		t.Errorf("output do job sem job-done: %q", out.(*ipc.ResponseJobOutput).Lines)
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
