# winmux — spec.md

**Criado:** 2026-02-18  
**Status:** v0.2.0 — funcional  
**Autor:** Christian + Emplares

---

## Problema

No Windows, não existe forma elegante de:
1. Anexar a uma sessão de terminal (PowerShell/cmd) já em execução remotamente
2. Injetar comandos em processos interativos rodando localmente
3. Listar sessões abertas com contexto útil (pasta, processo, histórico)
4. Ser notificado quando um processo longo termina

Soluções existentes exigem WSL (Linux), VNC (pesado), ou gambiarras com ConPTY sem abstração.

## Caso de uso real que originou o projeto:

Um usuário tinha o codex CLI e o Gemini CLI rodando em PowerShell numa máquina remota e precisava enviar prompts remotamente (ex - Zap - OpenClaw - SSH - winmux -> TUI) Não achei forma limpa de realizar esse fluxo, o que motivou o desenvolvimento do winmux. 

---

## Objetivo

Criar uma camada de abstração leve sobre ConPTY e sessões Windows Terminal que permita:
- Listar sessões interativas abertas
- Criar novas sessões
- Injetar input (texto ou teclas) programaticamente
- Ler output de sessões em andamento
- Detectar estado da sessão (idle, busy, waiting)
- Ser notificado via callback quando o estado muda

---

## Solução

Um daemon leve rodando em background no Windows que:
1. Cria e gerencia sessões ConPTY (PowerShell, cmd)
2. Expõe uma API local via named pipe (`\\.\pipe\winmux`)
3. Permite interagir via CLI (`winmux.exe`)

### Componentes

```
winmux-daemon.exe   → roda em background, gerencia sessões
winmux new          → cria nova sessão
winmux list         → lista sessões ativas com uptime e dir
winmux attach       → conecta com I/O ao vivo (streaming)
winmux send         → injeta comando (shell scripts)
winmux submit       → injeta texto em TUIs (Gemini, Codex)
winmux read [n]     → lê últimas n linhas do buffer
winmux status       → detecta estado: idle | busy | waiting
winmux reset        → envia Ctrl+C (limpa modo >> do PowerShell)
winmux watch        → polling + callback quando estado muda
winmux kill         → encerra sessão
```

---

## Stack

- **Go** — cross-compile nativo, binário único
- **ConPTY** — pseudo-terminal Windows (github.com/UserExistsError/conpty)
- **Named pipe** — IPC via go-winio (github.com/Microsoft/go-winio)
- **SSH** — transporte seguro externo (não faz parte do winmux)

---

## Detecção de estado

O `winmux status` analisa o buffer das últimas 20 linhas e detecta:

- **busy**: presença de spinner braille (⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏) ou ASCII (\|/-) repetido
- **waiting**: frases como "Do you trust", "Press enter", "? for shortcuts", opções numeradas
- **waiting**: modo de continuação `>>` do PowerShell
- **idle**: prompt PS ou > visível sem spinner

---

## Diferencial

- Foco em PowerShell nativo (não exige WSL)
- Leve — não é VNC, não compartilha tela
- Detecção de estado inteligente do buffer
- Callbacks configuráveis via `watch`
- Open source

---

## Roadmap

- [x] Daemon com named pipe
- [x] new, list, attach, send, read, kill
- [x] submit (TUI-aware)
- [x] status (idle/busy/waiting)
- [x] reset (Ctrl+C)
- [x] watch com callbacks
- [x] Serviço Windows (boot automático)
- [x] winmux key (teclas especiais: Escape, setas, Tab)
- [x] winmux logs (histórico completo)
- [x] Detecção de estado mais robusta

---

## v0.3.0 — Modo Agent (Jobs Assíncronos)

**Problema:** quando um agente de IA chama `winmux send` para executar algo demorado
(ex: Codex analisando um projeto inteiro por 30 minutos), o comando bloqueia sem sinalizar
progresso — comportamento deselegante para o usuário do outro lado.

**Solução:** um sistema de jobs assíncronos integrado ao daemon existente.

### Novos comandos

```
winmux job run <sessão> <comando>         → dispara o comando e retorna job ID imediatamente
winmux job status <job-id>               → idle | running | done | error
winmux job output <job-id> [--tail n]    → output acumulado até o momento
winmux job wait <job-id> [--timeout 30m] → bloqueia até concluir (com timeout)
winmux job list                          → todos os jobs ativos/recentes
winmux job kill <job-id>                 → cancela job em andamento
```

### Fluxo para agentes

```
1. agente chama: winmux job run minha-sessao "codex analyze ."
2. daemon retorna: job-id=abc123 (imediato)
3. agente notifica usuário: "Comando enviado (job abc123). Te aviso quando terminar."
4. agente faz polling: winmux job status abc123
5. quando done: winmux job output abc123 → entrega resultado
```

### Implementação

- Jobs são gerenciados pelo daemon existente (sem novo processo)
- Cada job tem: ID (uuid curto), sessão vinculada, estado, buffer de output, timestamps
- Polling ou watch: `winmux job wait` usa o mecanismo de watch existente internamente
- IPC: mesma named pipe, novos tipos de mensagem (`JobRun`, `JobStatus`, `JobOutput`)

### Distinção interativo vs. agent

Não são dois daemons — é um único daemon com dois modos de interface:

| Modo | Uso | Comportamento |
|------|-----|---------------|
| **Interativo** | Humano via terminal | `send`, `attach`, `submit` — bloqueante, síncrono |
| **Agent** | IA via SSH | `job run`, `job status`, `job output` — assíncrono |

### Compatibilidade

100% retrocompatível — comandos existentes não mudam.

---
