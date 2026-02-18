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

**Caso de uso real que originou o projeto:**  
Christian estava com o Codex CLI e o Gemini CLI rodando no PowerShell do Vaio/Beelink. Queria enviar prompts remotamente, via WhatsApp → Emplares → SSH → winmux → TUI. Não havia forma limpa de fazer isso.

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
- [ ] Serviço Windows (boot automático)
- [ ] winmux key (teclas especiais: Escape, setas, Tab)
- [ ] winmux logs (histórico completo)
- [ ] Detecção de estado mais robusta

---

## Notas

> "Eu gosto de coisas hack, mas é óbvio que é melhor uma solução mais elegante.  
> Esses momentos hacky são possíveis projetos — desenvolver e colocar no GitHub."  
> — Christian, 2026-02-18
