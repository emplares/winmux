// Testes de detecção de estado do buffer de sessão.
// Sem build tags — roda em Linux (sem ConPTY real).
package daemon

import (
	"testing"
)

// newTestBuf cria um sessionBuffer populado com as linhas dadas.
func newTestBuf(lines ...string) *sessionBuffer {
	sb := newSessionBuffer()
	for _, l := range lines {
		sb.appendLine(l)
	}
	return &sb
}

// ---------- DetectState ----------

func TestDetectState_EmptyBuffer(t *testing.T) {
	sb := newSessionBuffer()
	state, _ := sb.DetectState()
	if state != "idle" {
		t.Errorf("buffer vazio: esperava 'idle', obteve %q", state)
	}
}

func TestDetectState_IdlePrompt(t *testing.T) {
	cases := []string{
		`PS C:\>`,
		`PS C:\Users\chris>`,
		`PS C:\Projetos\winmux>`,
	}
	for _, prompt := range cases {
		sb := newTestBuf(
			"output da sessão",
			"mais output",
			prompt,
		)
		state, _ := sb.DetectState()
		if state != "idle" {
			t.Errorf("prompt %q: esperava 'idle', obteve %q", prompt, state)
		}
	}
}

func TestDetectState_BusyBrailleSpinner(t *testing.T) {
	// ⠋ é o primeiro char do spinner braille
	sb := newTestBuf(
		"loading something...",
		"⠋ processing",
	)
	state, _ := sb.DetectState()
	if state != "busy" {
		t.Errorf("spinner braille: esperava 'busy', obteve %q", state)
	}
}

func TestDetectState_BusyAllBrailleSpinners(t *testing.T) {
	spinners := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	for _, sp := range spinners {
		sb := newTestBuf("working... " + sp)
		state, _ := sb.DetectState()
		if state != "busy" {
			t.Errorf("spinner %q: esperava 'busy', obteve %q", sp, state)
		}
	}
}

func TestDetectState_BusyAsciiSpinner(t *testing.T) {
	// Precisa de mais de 10 chars de spinner ASCII para disparar
	sb := newTestBuf(
		`\ loading`,
		`| loading`,
		`/ loading`,
		`- loading`,
		`\ loading`,
		`| loading`,
		`/ loading`,
		`- loading`,
		`\ loading`,
		`| loading`,
		`/ loading`,
	)
	state, _ := sb.DetectState()
	if state != "busy" {
		t.Errorf("spinner ASCII: esperava 'busy', obteve %q", state)
	}
}

func TestDetectState_WaitingDoYouTrust(t *testing.T) {
	sb := newTestBuf(
		"Do you trust the files in this folder?",
		"1. Trust folder",
		"2. Don't trust",
	)
	state, detail := sb.DetectState()
	if state != "waiting" {
		t.Errorf("'Do you trust': esperava 'waiting', obteve %q", state)
	}
	// detail deve conter as opções numeradas
	if detail == "" {
		t.Error("detail deveria conter as opções numeradas")
	}
}

func TestDetectState_WaitingYN(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{"[Y/n]", "Continue? [Y/n]"},
		{"(y/n)", "Overwrite? (y/n)"},
		{"[y/n] lowercase", "proceed? [y/n]"},
	}
	for _, tc := range cases {
		sb := newTestBuf(tc.line)
		state, _ := sb.DetectState()
		if state != "waiting" {
			t.Errorf("%s: esperava 'waiting', obteve %q", tc.name, state)
		}
	}
}

func TestDetectState_WaitingPressEnter(t *testing.T) {
	sb := newTestBuf("Press Enter to continue...")
	state, _ := sb.DetectState()
	if state != "waiting" {
		t.Errorf("'Press Enter to continue': esperava 'waiting', obteve %q", state)
	}
}

func TestDetectState_WaitingPressEsc(t *testing.T) {
	sb := newTestBuf("Press Esc to cancel")
	state, _ := sb.DetectState()
	if state != "waiting" {
		t.Errorf("'Press Esc': esperava 'waiting', obteve %q", state)
	}
}

func TestDetectState_WaitingUpdateAvailable(t *testing.T) {
	sb := newTestBuf(
		"Update available v1.0.0 → v2.0.0",
	)
	state, _ := sb.DetectState()
	if state != "waiting" {
		t.Errorf("'Update available': esperava 'waiting', obteve %q", state)
	}
}

func TestDetectState_WaitingPowerShellContinuation(t *testing.T) {
	sb := newTestBuf(
		`if ($true) {`,
		`>>`,
	)
	state, detail := sb.DetectState()
	if state != "waiting" {
		t.Errorf("'>>': esperava 'waiting', obteve %q", state)
	}
	if detail == "" {
		t.Error("detail deveria mencionar o modo de continuação")
	}
}

func TestDetectState_ErrorLineIsIdle(t *testing.T) {
	// O código atual não tem estado "error" — deve retornar "idle"
	// (documentar comportamento atual, não expectativa futura)
	sb := newTestBuf(
		"Error: something failed",
		`PS C:\>`,
	)
	state, _ := sb.DetectState()
	// Depois de uma mensagem de erro com prompt disponível, é idle
	if state != "idle" {
		t.Errorf("erro + prompt: esperava 'idle', obteve %q", state)
	}
}

func TestDetectState_ErrorLineWithoutPrompt(t *testing.T) {
	// Erro sem prompt — ainda é "idle" (não há estado "error" implementado)
	sb := newTestBuf("Error: something failed")
	state, _ := sb.DetectState()
	// Aceita tanto "idle" quanto "error" (documentação de comportamento)
	if state != "idle" && state != "error" {
		t.Errorf("linha de erro: esperava 'idle' ou 'error', obteve %q", state)
	}
}

// ---------- appendOutput ----------

func TestAppendOutput_SplitsLines(t *testing.T) {
	sb := newSessionBuffer()
	sb.appendOutput([]byte("linha1\nlinha2\nlinha3\n"))

	lines := sb.readBuffer(10)
	if len(lines) != 3 {
		t.Fatalf("esperava 3 linhas, obteve %d: %v", len(lines), lines)
	}
	if lines[0] != "linha1" || lines[1] != "linha2" || lines[2] != "linha3" {
		t.Errorf("linhas incorretas: %v", lines)
	}
}

func TestAppendOutput_PartialLine(t *testing.T) {
	sb := newSessionBuffer()
	sb.appendOutput([]byte("parcial"))
	// Linha incompleta não vai para o buffer ainda
	lines := sb.readBuffer(10)
	if len(lines) != 0 {
		t.Errorf("linha parcial não deveria estar no buffer: %v", lines)
	}
	// Completa a linha
	sb.appendOutput([]byte(" completa\n"))
	lines = sb.readBuffer(10)
	if len(lines) != 1 || lines[0] != "parcial completa" {
		t.Errorf("linha parcial+completa incorreta: %v", lines)
	}
}

func TestReadBuffer_LimitN(t *testing.T) {
	sb := newTestBuf("l1", "l2", "l3", "l4", "l5")
	lines := sb.readBuffer(3)
	if len(lines) != 3 {
		t.Fatalf("esperava 3, obteve %d", len(lines))
	}
	// Deve ser as ÚLTIMAS 3 linhas
	if lines[0] != "l3" || lines[1] != "l4" || lines[2] != "l5" {
		t.Errorf("últimas 3 linhas incorretas: %v", lines)
	}
}

func TestReadBuffer_Zero(t *testing.T) {
	sb := newTestBuf("l1", "l2")
	lines := sb.readBuffer(0)
	if len(lines) != 2 {
		t.Errorf("n=0 deveria retornar tudo (2 linhas), obteve %d", len(lines))
	}
}

// ---------- stripAnsi ----------

func TestStripAnsi(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"\x1b[32mhello\x1b[0m", "hello"},
		{"\x1b[1;31mERROR\x1b[0m: failed", "ERROR: failed"},
		{"plain text", "plain text"},
		{"\x1b]0;title\x07text", "text"},
		{"no escape", "no escape"},
	}
	for _, tc := range cases {
		got := stripAnsi(tc.input)
		if got != tc.want {
			t.Errorf("stripAnsi(%q) = %q, queria %q", tc.input, got, tc.want)
		}
	}
}

// ---------- extractTimeHint ----------

func TestExtractTimeHint(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"building... 5m 44s", "5m 44s remaining"},
		{"waiting 30s for response", "30s remaining"},
		{"no time here", ""},
		{"2m 10s left", "2m 10s remaining"},
	}
	for _, tc := range cases {
		got := extractTimeHint(tc.input)
		if got != tc.want {
			t.Errorf("extractTimeHint(%q) = %q, queria %q", tc.input, got, tc.want)
		}
	}
}

// ---------- logs ----------

func TestLogs_MaxLines(t *testing.T) {
	sb := newSessionBuffer()
	// Adiciona 600 linhas
	for i := 0; i < 600; i++ {
		sb.appendLine("linha")
	}
	logs := sb.logs()
	if len(logs) != 500 {
		t.Errorf("logs deveria limitar a 500, obteve %d", len(logs))
	}
}
