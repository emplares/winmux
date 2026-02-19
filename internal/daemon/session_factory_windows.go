//go:build windows

// session_factory_windows.go — implementação Windows de ISession e defaultSessionFactory.
package daemon

import "time"

// Garantia de compilação: Session deve implementar ISession.
var _ ISession = (*Session)(nil)

func (s *Session) GetID() string              { return s.ID }
func (s *Session) GetPID() int                { return s.PID }
func (s *Session) GetShell() string           { return s.Shell }
func (s *Session) GetDir() string             { return s.Dir }
func (s *Session) GetCreatedAt() time.Time    { return s.CreatedAt }
func (s *Session) GetStatus() string          { return s.Status }

// defaultSessionFactory cria uma Session real com ConPTY.
func defaultSessionFactory(id, shell string) (ISession, error) {
	return NewSession(id, shell)
}
