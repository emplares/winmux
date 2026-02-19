// Testes de serialização/deserialização do protocolo IPC.
// Sem build tags — roda em qualquer GOOS.
package ipc

import (
	"encoding/json"
	"testing"
	"time"
)

// ---------- Request ----------

func TestRequestMarshal(t *testing.T) {
	req := Request{
		Action: "send",
		ID:     "abc123",
		Input:  "echo hello",
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal falhou: %v", err)
	}

	var got Request
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal falhou: %v", err)
	}
	if got.Action != req.Action {
		t.Errorf("Action: queria %q, obteve %q", req.Action, got.Action)
	}
	if got.ID != req.ID {
		t.Errorf("ID: queria %q, obteve %q", req.ID, got.ID)
	}
	if got.Input != req.Input {
		t.Errorf("Input: queria %q, obteve %q", req.Input, got.Input)
	}
}

func TestRequestOmitempty(t *testing.T) {
	req := Request{Action: "list"}
	data, _ := json.Marshal(req)

	// Campos omitempty vazios não devem aparecer no JSON
	raw := string(data)
	for _, field := range []string{`"id"`, `"input"`, `"shell"`, `"key"`} {
		if contains(raw, field) {
			t.Errorf("campo vazio %s não deveria aparecer no JSON: %s", field, raw)
		}
	}
}

func TestRequestAllFields(t *testing.T) {
	req := Request{
		Action:  "watch",
		Shell:   "powershell",
		ID:      "ff00aa",
		Input:   "Get-Date",
		Key:     "ctrl+c",
		Lines:   50,
		OnIdle:  "echo idle",
		OnWait:  "echo wait",
		Timeout: 120,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got Request
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got != req {
		t.Errorf("round-trip diverge:\n  queria: %+v\n  obteve: %+v", req, got)
	}
}

// ---------- Response types ----------

func TestResponseNew(t *testing.T) {
	resp := ResponseNew{SessionID: "deadbeef", PID: 1234}
	roundTrip(t, resp, &ResponseNew{}, func(v interface{}) {
		got := v.(*ResponseNew)
		if got.SessionID != resp.SessionID {
			t.Errorf("SessionID: queria %q, obteve %q", resp.SessionID, got.SessionID)
		}
		if got.PID != resp.PID {
			t.Errorf("PID: queria %d, obteve %d", resp.PID, got.PID)
		}
	})
}

func TestResponseList(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	resp := ResponseList{
		Sessions: []SessionInfo{
			{ID: "a1b2c3", PID: 42, Shell: "powershell", Dir: `C:\`, CreatedAt: now, Status: "active"},
		},
	}
	data, _ := json.Marshal(resp)
	var got ResponseList
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(got.Sessions) != 1 {
		t.Fatalf("esperava 1 sessão, obteve %d", len(got.Sessions))
	}
	s := got.Sessions[0]
	if s.ID != "a1b2c3" || s.PID != 42 || s.Shell != "powershell" {
		t.Errorf("SessionInfo diverge: %+v", s)
	}
}

func TestResponseListEmpty(t *testing.T) {
	resp := ResponseList{Sessions: []SessionInfo{}}
	data, _ := json.Marshal(resp)
	var got ResponseList
	json.Unmarshal(data, &got)
	if got.Sessions == nil {
		t.Error("Sessions deveria ser slice vazio, não nil")
	}
	if len(got.Sessions) != 0 {
		t.Errorf("esperava 0 sessões, obteve %d", len(got.Sessions))
	}
}

func TestResponseKill(t *testing.T) {
	resp := ResponseKill{OK: true}
	roundTrip(t, resp, &ResponseKill{}, func(v interface{}) {
		got := v.(*ResponseKill)
		if !got.OK {
			t.Error("OK deveria ser true")
		}
	})
}

func TestResponseKillError(t *testing.T) {
	resp := ResponseKill{Error: "sessão não encontrada"}
	data, _ := json.Marshal(resp)
	var got ResponseKill
	json.Unmarshal(data, &got)
	if got.OK {
		t.Error("OK deveria ser false")
	}
	if got.Error == "" {
		t.Error("Error não deveria ser vazio")
	}
}

func TestResponseSend(t *testing.T) {
	resp := ResponseSend{OK: true}
	roundTrip(t, resp, &ResponseSend{}, func(v interface{}) {
		got := v.(*ResponseSend)
		if !got.OK {
			t.Error("OK deveria ser true")
		}
	})
}

func TestResponseRead(t *testing.T) {
	resp := ResponseRead{Lines: []string{"linha1", "linha2", "PS C:\\>"}}
	data, _ := json.Marshal(resp)
	var got ResponseRead
	json.Unmarshal(data, &got)
	if len(got.Lines) != 3 {
		t.Fatalf("esperava 3 linhas, obteve %d", len(got.Lines))
	}
	if got.Lines[2] != "PS C:\\>" {
		t.Errorf("terceira linha errada: %q", got.Lines[2])
	}
}

func TestResponseStatus(t *testing.T) {
	cases := []ResponseStatus{
		{State: "idle"},
		{State: "busy", Detail: "5m 44s remaining"},
		{State: "waiting", Detail: "1. Trust folder"},
		{State: "idle", Error: "sessão não encontrada"},
	}
	for _, resp := range cases {
		data, _ := json.Marshal(resp)
		var got ResponseStatus
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("Unmarshal %+v: %v", resp, err)
		}
		if got.State != resp.State {
			t.Errorf("State: queria %q, obteve %q", resp.State, got.State)
		}
		if got.Detail != resp.Detail {
			t.Errorf("Detail: queria %q, obteve %q", resp.Detail, got.Detail)
		}
	}
}

func TestResponseAttach(t *testing.T) {
	resp := ResponseAttach{OK: true, SessionID: "abc123"}
	roundTrip(t, resp, &ResponseAttach{}, func(v interface{}) {
		got := v.(*ResponseAttach)
		if !got.OK || got.SessionID != "abc123" {
			t.Errorf("diverge: %+v", got)
		}
	})
}

func TestResponseWatch(t *testing.T) {
	resp := ResponseWatch{OK: true}
	data, _ := json.Marshal(resp)
	var got ResponseWatch
	json.Unmarshal(data, &got)
	if !got.OK {
		t.Error("OK deveria ser true")
	}
}

func TestResponseLogs(t *testing.T) {
	resp := ResponseLogs{Lines: []string{"[INFO] started", "[WARN] slow"}}
	data, _ := json.Marshal(resp)
	var got ResponseLogs
	json.Unmarshal(data, &got)
	if len(got.Lines) != 2 {
		t.Fatalf("esperava 2 linhas, obteve %d", len(got.Lines))
	}
}

func TestResponseKey(t *testing.T) {
	resp := ResponseKey{OK: true}
	data, _ := json.Marshal(resp)
	var got ResponseKey
	json.Unmarshal(data, &got)
	if !got.OK {
		t.Error("OK deveria ser true")
	}
}

// ---------- helpers ----------

func roundTrip(t *testing.T, in interface{}, out interface{}, check func(interface{})) {
	t.Helper()
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	check(out)
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
