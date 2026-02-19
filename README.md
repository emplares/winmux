# winmux

> Controle remoto de sessões PowerShell no Windows — sem VNC, sem WSL obrigatório.

**winmux** é uma camada leve sobre ConPTY que permite criar, listar, injetar comandos e monitorar sessões de terminal Windows remotamente via SSH.

Nasceu de um caso de uso real: controlar o Codex CLI e o Gemini CLI rodando no Windows a partir de um agente de IA na nuvem, via WhatsApp.

---

## O problema

No Windows, não existe forma elegante de:
- Injetar comandos em sessões de terminal já abertas remotamente
- Monitorar o estado de um processo interativo (busy? esperando input?)
- Ser notificado quando um agente de IA termina uma tarefa longa

Soluções existentes exigem WSL, VNC, ou gambiarras com ConPTY sem abstração.

## A solução

Um daemon leve (named pipe) + CLI simples:

```
winmux-daemon.exe   → roda em background, gerencia sessões via ConPTY
winmux.exe          → CLI para interagir com as sessões
```

Transporte seguro via SSH. Sem dependências externas além do Go.

---

## Instalação

### Pré-requisitos
- Windows 10 1903+ ou Windows 11 (ConPTY requerido)
- SSH configurado no Windows (OpenSSH Server)

### Download

Baixe os binários em [Releases](../../releases) ou compile do fonte:

```bash
GOOS=windows GOARCH=amd64 go build -o dist/winmux.exe ./cmd/winmux
GOOS=windows GOARCH=amd64 go build -o dist/winmux-daemon.exe ./cmd/daemon
```

### Iniciar o daemon

```powershell
# Manualmente (foreground)
.\winmux-daemon.exe

# Em background (recomendado)
Start-Process -FilePath .\winmux-daemon.exe -WindowStyle Hidden
```

### Instalação como Serviço Windows

Para que o daemon inicie automaticamente no boot do Windows, instale-o como serviço do sistema:

```powershell
# Instalar o serviço (requer privilégios de Administrador)
.\winmux-daemon.exe --install-service

# Iniciar o serviço manualmente (ou reinicie o Windows)
sc start WinMuxDaemon

# Verificar status
sc query WinMuxDaemon

# Parar o serviço
sc stop WinMuxDaemon

# Remover o serviço
.\winmux-daemon.exe --remove-service
```

> **Nota:** Execute o PowerShell como **Administrador** para instalar/remover serviços.

O serviço é registrado como `WinMuxDaemon` com início automático (`StartType=Automatic`), rodando como `LocalSystem`. Após instalado, o daemon sobe automaticamente a cada boot, sem necessidade de login manual.

---

## Uso

```
winmux new [--shell powershell|cmd]          Cria nova sessão
winmux list                                  Lista sessões ativas
winmux attach <id>                           Conecta à sessão (I/O ao vivo)
winmux send <id> <comando>                   Injeta comando (shell)
winmux submit <id> <texto>                   Injeta texto em TUI (Gemini, Codex)
winmux read <id> [linhas]                    Lê buffer de output (padrão: 50 linhas)
winmux logs <id>                             Histórico completo do buffer (até 500 linhas)
winmux status <id>                           Estado: idle | busy | waiting
winmux reset <id>                            Envia Ctrl+C (limpa >>)
winmux key <id> <tecla>                      Envia tecla especial (esc, ctrl+c, up, down…)
winmux watch <id> [opções]                   Notifica quando estado muda
winmux kill <id>                             Encerra sessão
winmux version                               Versão
```

### Exemplos

```powershell
# Criar sessão e rodar um comando
winmux new
winmux send abc123 "Get-Date"
winmux read abc123

# Interagir com Gemini CLI ou Codex CLI (TUI)
winmux submit abc123 "resuma os PDFs desta pasta"

# Verificar estado
winmux status abc123
# → busy (5m 44s remaining)
# → waiting (1. Trust folder | 2. Trust parent | 3. Don't trust)
# → idle

# Limpar modo de continuação >> do PowerShell
winmux reset abc123

# Notificar via webhook quando terminar
winmux watch abc123 --on-idle "curl -X POST https://meu-webhook.com/notify"

# Notificar quando precisar de input do usuário
winmux watch abc123 --on-waiting "curl -X POST https://meu-webhook.com/aguardando"

# Watch com timeout
winmux watch abc123 --on-idle "echo PRONTO" --timeout 300
```

---

## Caso de uso: agente de IA remoto

```
[WhatsApp] → [Agente na nuvem] → SSH → winmux → [Gemini/Codex no Windows]
```

1. Agente cria sessão: `winmux new`
2. Abre o CLI: `winmux send <id> "gemini --yolo"`
3. Submete prompt: `winmux submit <id> "analise esses arquivos"`
4. Monitora: `winmux watch <id> --on-idle "curl -X POST $WEBHOOK"`
5. Lê resultado: `winmux read <id>`

---

## Detecção de estado (`winmux status`)

O daemon analisa o buffer de output da sessão e detecta:

| Estado | Descrição |
|--------|-----------|
| `idle` | Prompt disponível, pronto para receber comandos |
| `busy` | Spinner detectado (⠋⠙⠹ ou \|/-), processo em andamento |
| `waiting` | Aguardando input do usuário (Trust folder?, Press enter, etc.) |

---

## Stack

- **Go** — cross-compile nativo, binário único, sem runtime
- **ConPTY** — pseudo-terminal Windows nativo
- **Named pipe** — IPC local eficiente (`\\.\pipe\winmux`)
- **SSH** — transporte seguro (sem servidor HTTP exposto)

---

## Roadmap

- [x] Serviço Windows (sobe automaticamente no boot) — `--install-service`
- [x] `winmux logs <id>` — histórico completo da sessão (até 500 linhas)
- [x] `winmux key <id> <tecla>` — enviar teclas especiais (Ctrl+C, Escape, setas)
- [ ] Detecção de estado mais robusta (ML leve sobre o buffer)
- [ ] Suporte a WSL

---

## Origem

> "Eu gosto de coisas hack, mas é óbvio que é melhor uma solução mais elegante.
> Esses momentos hacky são possíveis projetos — desenvolver e colocar no GitHub."
> — Christian, 2026-02-18

Desenvolvido por Christian + Emplares em Garopaba, SC.

---

## Licença

MIT
