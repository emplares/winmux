// Package ipc define o protocolo de mensagens JSON entre CLI e daemon.
package ipc

import "time"

// Version é a versão de CLI e daemon (mesmo protocolo).
const Version = "0.3.0"

// Request representa um comando enviado pelo CLI ao daemon.
type Request struct {
	Action  string `json:"action"` // "new", "list", "kill", "send", "submit", "attach", "read", "logs", "status", "reset", "watch", "key", "job.run", "job.status", "job.output", "job.list", "job.kill", "job.wait"
	Shell   string `json:"shell,omitempty"`
	ID      string `json:"id,omitempty"`
	Input   string `json:"input,omitempty"`   // para "send"/"submit"/"job.run"
	Key     string `json:"key,omitempty"`     // para "key": "ctrl+c", "esc", "up", "down", "tab", "enter", "ctrl+d"
	Lines   int    `json:"lines,omitempty"`   // para "read" (quantas linhas do buffer) e "job.output" (tail)
	OnIdle  string `json:"on_idle,omitempty"` // para "watch"
	OnWait  string `json:"on_wait,omitempty"` // para "watch"
	Timeout int    `json:"timeout,omitempty"` // para "watch" e "job.wait" (segundos)
}

// JobInfo contém os dados de um job para exibição no CLI.
type JobInfo struct {
	ID        string     `json:"id"`
	SessionID string     `json:"session_id"`
	Command   string     `json:"command"`
	State     string     `json:"state"` // "running" | "done" | "error"
	Lines     int        `json:"lines"` // total de linhas de output acumuladas
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// ResponseJobRun é a resposta ao comando "job.run".
type ResponseJobRun struct {
	JobID string `json:"job_id"`
	Error string `json:"error,omitempty"`
}

// ResponseJobStatus é a resposta ao comando "job.status".
type ResponseJobStatus struct {
	Job   JobInfo `json:"job"`
	Error string  `json:"error,omitempty"`
}

// ResponseJobOutput é a resposta ao comando "job.output".
type ResponseJobOutput struct {
	Lines []string `json:"lines"`
	Error string   `json:"error,omitempty"`
}

// ResponseJobList é a resposta ao comando "job.list".
type ResponseJobList struct {
	Jobs  []JobInfo `json:"jobs"`
	Error string    `json:"error,omitempty"`
}

// ResponseJobKill é a resposta ao comando "job.kill".
type ResponseJobKill struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// ResponseKey é a resposta ao comando "key".
type ResponseKey struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// ResponseSend é a resposta ao comando "send"/"submit".
type ResponseSend struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// ResponseRead é a resposta ao comando "read".
type ResponseRead struct {
	Lines []string `json:"lines"`
	Error string   `json:"error,omitempty"`
}

// ResponseAttach sinaliza que o daemon aceitou o attach.
// Após isso, o protocolo muda para streaming raw (não JSON).
type ResponseAttach struct {
	OK        bool   `json:"ok"`
	SessionID string `json:"session_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

// SessionInfo contém os dados de uma sessão para exibição no CLI.
type SessionInfo struct {
	ID        string    `json:"id"`
	PID       int       `json:"pid"`
	Shell     string    `json:"shell"`
	Dir       string    `json:"dir"`
	CreatedAt time.Time `json:"created_at"`
	Status    string    `json:"status"`
}

// ResponseNew é a resposta ao comando "new".
type ResponseNew struct {
	SessionID string `json:"session_id"`
	PID       int    `json:"pid"`
	Error     string `json:"error,omitempty"`
}

// ResponseList é a resposta ao comando "list".
type ResponseList struct {
	Sessions []SessionInfo `json:"sessions"`
	Error    string        `json:"error,omitempty"`
}

// ResponseKill é a resposta ao comando "kill".
type ResponseKill struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// ResponseStatus é a resposta ao comando "status".
type ResponseStatus struct {
	State  string `json:"state"`            // "idle", "busy", "waiting", "error"
	Detail string `json:"detail,omitempty"` // ex: "5m 44s remaining" ou linha de erro
	Error  string `json:"error,omitempty"`  // erro de protocolo (não confundir com State=="error")
}

// ResponseWatch é a resposta ao comando "watch" (confirmação de início).
type ResponseWatch struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// ResponseLogs é a resposta ao comando "logs".
type ResponseLogs struct {
	Lines []string `json:"lines"`
	Error string   `json:"error,omitempty"`
}
