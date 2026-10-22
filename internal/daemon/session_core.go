// session_core.go — lógica pura de buffer e detecção de estado.
// Sem build tags: compila em qualquer GOOS (Linux, Windows, etc).
package daemon

import (
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const bufferMaxLines = 10000

// sessionBuffer encapsula o buffer circular de linhas de output e a lógica
// de detecção de estado. Reutilizado por Session (Windows) e por testes (Linux).
type sessionBuffer struct {
	mu              sync.RWMutex
	buffer          []string
	line            []byte // linha corrente ainda sem '\n' (ex: o prompt "PS C:\> ")
	total           int    // linhas completas já vistas desde o início (cursor absoluto)
	subs            map[int]io.Writer
	nextSub         int
	LastStateChange time.Time
	lastState       string
}

func newSessionBuffer() sessionBuffer {
	return sessionBuffer{
		subs:            map[int]io.Writer{},
		LastStateChange: time.Now(),
		lastState:       "idle",
	}
}

// appendOutput adiciona bytes ao buffer de linhas e repassa o output bruto
// aos clientes em attach (thread-safe). captureOutput é o único leitor do
// ConPTY; attach recebe uma cópia em vez de competir pela leitura.
func (sb *sessionBuffer) appendOutput(data []byte) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	// ponytail: escrita síncrona — um cliente lento em attach atrasa a captura; fila por cliente se isso aparecer.
	for id, w := range sb.subs {
		if _, err := w.Write(data); err != nil {
			delete(sb.subs, id)
		}
	}
	for _, b := range data {
		if b == '\n' {
			sb.pushLine(strings.TrimSuffix(string(sb.line), "\r"))
			sb.line = sb.line[:0]
		} else {
			sb.line = append(sb.line, b)
		}
	}
}

// appendLine adiciona uma linha já completa ao buffer (helper para testes).
func (sb *sessionBuffer) appendLine(line string) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.pushLine(line)
}

// pushLine exige sb.mu travado.
func (sb *sessionBuffer) pushLine(line string) {
	sb.buffer = append(sb.buffer, line)
	sb.total++
	if len(sb.buffer) > bufferMaxLines {
		sb.buffer = sb.buffer[1:]
	}
}

// subscribe registra w para receber o output bruto; devolve a função que cancela.
func (sb *sessionBuffer) subscribe(w io.Writer) (unsubscribe func()) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	id := sb.nextSub
	sb.nextSub++
	sb.subs[id] = w
	return func() {
		sb.mu.Lock()
		delete(sb.subs, id)
		sb.mu.Unlock()
	}
}

// ReadSince devolve as linhas completas com índice absoluto >= cursor e o
// próximo cursor. Linhas já descartadas do buffer circular são puladas.
func (sb *sessionBuffer) ReadSince(cursor int) ([]string, int) {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	first := sb.total - len(sb.buffer) // índice absoluto de buffer[0]
	if cursor < first {
		cursor = first
	}
	if cursor >= sb.total {
		return nil, sb.total
	}
	lines := make([]string, sb.total-cursor)
	copy(lines, sb.buffer[cursor-first:])
	return lines, sb.total
}

// PromptVisible indica se a linha corrente (sem '\n') termina num prompt de
// shell: "PS C:\> " (PowerShell) ou "C:\>" (cmd).
func (sb *sessionBuffer) PromptVisible() bool {
	sb.mu.RLock()
	line := string(sb.line)
	sb.mu.RUnlock()
	return isPrompt(line)
}

func isPrompt(line string) bool {
	l := strings.TrimSpace(stripAnsi(line))
	return strings.HasSuffix(l, ">") && !strings.HasSuffix(l, ">>")
}

// readBuffer retorna as últimas n linhas (thread-safe).
func (sb *sessionBuffer) readBuffer(n int) []string {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	if n <= 0 || n > len(sb.buffer) {
		n = len(sb.buffer)
	}
	lines := make([]string, n)
	copy(lines, sb.buffer[len(sb.buffer)-n:])
	return lines
}

// logs retorna as últimas 500 linhas (thread-safe).
func (sb *sessionBuffer) logs() []string {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	const maxLogs = 500
	n := len(sb.buffer)
	if n > maxLogs {
		n = maxLogs
	}
	lines := make([]string, n)
	copy(lines, sb.buffer[len(sb.buffer)-n:])
	return lines
}

// DetectState analisa o buffer recente e retorna ("idle"|"busy"|"waiting"|"error", detail).
func (sb *sessionBuffer) DetectState() (state, detail string) {
	sb.mu.RLock()
	lines := sb.buffer
	if len(sb.line) > 0 {
		// A linha corrente (prompt, "(y/n)", ">>") ainda não tem '\n' mas é a
		// que mais diz sobre o estado.
		lines = append(lines[:len(lines):len(lines)], string(sb.line))
	}
	sb.mu.RUnlock()

	if len(lines) == 0 {
		return "idle", ""
	}

	// Analisa as últimas 30 linhas (ou menos)
	lookback := 30
	if len(lines) < lookback {
		lookback = len(lines)
	}
	recent := lines[len(lines)-lookback:]

	combined := strings.ToLower(strings.Join(recent, "\n"))
	combinedStripped := stripAnsi(combined)

	// 1. Detecta spinner braille (busy)
	for _, ch := range spinnerChars {
		if strings.ContainsRune(combined, ch) {
			detail = extractTimeHint(combinedStripped)
			return "busy", detail
		}
	}

	// Spinner ASCII: só conta \ | / - redesenhado no mesmo lugar (após \r,
	// backspace ou movimento de cursor do ConPTY). Contar esses chars soltos
	// marcava como busy qualquer sessão com caminhos (C:\Users) ou cmdlets.
	if len(asciiSpinnerRe.FindAllStringIndex(combined, 3)) >= 3 {
		detail = extractTimeHint(combinedStripped)
		return "busy", detail
	}

	// 2. Detecta progress bars estilo [====>   ] ou █████░░░
	if strings.ContainsAny(combinedStripped, "█░") || containsProgressBar(combinedStripped) {
		detail = extractTimeHint(combinedStripped)
		return "busy", detail
	}

	// 3. Detecta frases de busy
	for _, phrase := range busyPhrases {
		if strings.Contains(combinedStripped, phrase) {
			detail = extractTimeHint(combinedStripped)
			return "busy", detail
		}
	}

	// 4. Detecta contadores de progresso [1/10], (2/5)
	if containsProgressCounter(combinedStripped) {
		detail = extractTimeHint(combinedStripped)
		return "busy", detail
	}

	// 5. Detecta waiting (aguardando input)
	for _, phrase := range waitingPhrases {
		if strings.Contains(combinedStripped, phrase) {
			detail = extractOptions(recent)
			return "waiting", detail
		}
	}

	// 6. Detecta modo de continuação ">>" do PowerShell
	lastLine := strings.TrimSpace(stripAnsi(lines[len(lines)-1]))
	if lastLine == ">>" || strings.HasSuffix(lastLine, ">> ") {
		return "waiting", "PowerShell continuation mode (>>). Use: winmux reset <id>"
	}

	// 7. Detecta estado de erro na última linha não-vazia relevante
	for i := len(recent) - 1; i >= 0; i-- {
		stripped := strings.TrimSpace(stripAnsi(recent[i]))
		if stripped == "" {
			continue
		}
		strippedLower := strings.ToLower(stripped)
		for _, pat := range errorPatterns {
			if strings.Contains(strippedLower, pat) {
				return "error", stripped
			}
		}
		break
	}

	// 8. Idle — prompt disponível
	return "idle", ""
}

// ---------- constantes e helpers (sem dependência de Windows) ----------

// spinnerChars são os caracteres braille de spinner usados por CLIs modernos.
const spinnerChars = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"

// asciiSpinnerRe casa um frame de spinner ASCII (npm e outros): o char logo
// após \r, backspace ou um CSI de posicionamento de cursor (texto já em minúsculas).
var asciiSpinnerRe = regexp.MustCompile("(?:\r|\x08|\x1b\\[[0-9;]*[HGCDFhgcdf])[\\\\|/-]")

// waitingPhrases são frases que indicam que a sessão aguarda input do usuário.
// Nota: todas em minúsculas — a comparação usa combinedStripped (lowercase).
var waitingPhrases = []string{
	"do you trust",
	"press enter to continue",
	"press enter",
	"(y/n)",
	"(yes/no)",
	"[y/n]",
	"trust folder",
	"trust parent",
	"don't trust",
	"update available",
	"? for shortcuts",
	"press esc",
	"password:",
	"enter password",
	"pin:",
	"--more--",
}

// busyPhrases são frases que indicam que a sessão está executando algo.
var busyPhrases = []string{
	"... (running)",
	"downloading",
	"installing",
	"building",
}

// errorPatterns são padrões que indicam erro no output (comparados em lowercase).
var errorPatterns = []string{
	"error:",
	"exception:",
	"failed",
	"fatal:",
}

// stripAnsi remove sequências de escape ANSI de uma string.
func stripAnsi(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		_, size := utf8.DecodeRuneInString(s[i:])
		if s[i] == '\x1b' {
			i++
			if i < len(s) && s[i] == '[' {
				// CSI sequence: ESC [ ... final
				i++
				for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
					i++
				}
				if i < len(s) {
					i++
				}
			} else if i < len(s) && s[i] == ']' {
				// OSC sequence: ESC ] ... BEL or ST
				i++
				for i < len(s) && s[i] != '\x07' && !(s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '\\') {
					i++
				}
				if i < len(s) {
					i++
				}
			}
		} else {
			b.WriteByte(s[i])
			i += size
		}
	}
	return b.String()
}

// containsProgressBar detecta progress bars ASCII estilo [====>   ] ou [####  ].
func containsProgressBar(s string) bool {
	inBracket := false
	eqCount := 0
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '[' {
			inBracket = true
			eqCount = 0
		} else if inBracket {
			if ch == '=' || ch == '#' || ch == '>' || ch == '-' {
				eqCount++
			} else if ch == ']' {
				if eqCount >= 3 {
					return true
				}
				inBracket = false
			} else if ch == ' ' {
				// espaços são permitidos dentro da barra
			} else {
				inBracket = false
			}
		}
	}
	return false
}

// containsProgressCounter detecta padrões de contador como [1/10] ou (2/5).
func containsProgressCounter(s string) bool {
	for i := 0; i < len(s)-3; i++ {
		open := s[i]
		close := byte(']')
		if open == '(' {
			close = ')'
		} else if open != '[' {
			continue
		}
		j := i + 1
		slashPos := -1
		for j < len(s) && s[j] != byte(close) && j-i < 12 {
			if s[j] == '/' {
				slashPos = j
			}
			j++
		}
		if j < len(s) && s[j] == byte(close) && slashPos > i+1 {
			before := strings.TrimSpace(s[i+1 : slashPos])
			after := strings.TrimSpace(s[slashPos+1 : j])
			if isDigits(before) && isDigits(after) {
				return true
			}
		}
	}
	return false
}

// extractTimeHint tenta extrair uma estimativa de tempo de uma string.
func extractTimeHint(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 1 && w[len(w)-1] == 's' && isDigits(w[:len(w)-1]) {
			if i > 0 && len(words[i-1]) > 1 && words[i-1][len(words[i-1])-1] == 'm' {
				return words[i-1] + " " + w + " remaining"
			}
			return w + " remaining"
		}
	}
	return ""
}

// extractOptions extrai opções numeradas visíveis do buffer.
func extractOptions(lines []string) string {
	var opts []string
	for _, l := range lines {
		stripped := stripAnsi(strings.TrimSpace(l))
		if len(stripped) > 2 && (stripped[0] >= '1' && stripped[0] <= '9') && stripped[1] == '.' {
			opts = append(opts, stripped)
		}
	}
	if len(opts) > 0 {
		return strings.Join(opts, " | ")
	}
	return ""
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}
