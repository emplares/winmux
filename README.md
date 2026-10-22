# winmux

[![ci](https://github.com/emplares/winmux/actions/workflows/ci.yml/badge.svg)](https://github.com/emplares/winmux/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**tmux-style persistent sessions for native Windows PowerShell, built so a remote AI agent can drive interactive CLIs over SSH.**

A small daemon keeps PowerShell or cmd sessions alive in ConPTY, Windows' native pseudo-terminal. A CLI talks to it over a local named pipe. You can create sessions and type into them, read their output and ask whether a session is idle, busy or waiting for input, all from a plain SSH command. No VNC, no WSL, no screen sharing.

## Why it exists

I had Codex CLI and Gemini CLI running in PowerShell on a Windows laptop and wanted a cloud agent to send them prompts, from a chat message through SSH into the running terminal UI. On Linux that's `tmux send-keys` and `tmux capture-pane`. Windows has nothing like it: OpenSSH gives you a fresh shell each time, and there's no way to type into a terminal session that's already running.

```
chat ─► cloud agent ─► ssh ─► winmux.exe ─► \\.\pipe\winmux ─► winmux-daemon.exe ─► ConPTY ─► PowerShell / Codex / Gemini
```

## What an agent does with it

An example session (IDs and timings are illustrative):

```powershell
PS> winmux new
✓ Sessão criada: 3fa91c (PID 7412)

PS> winmux job run 3fa91c "npm test"          # returns immediately
✓ Job iniciado: b20e7d

PS> winmux job wait b20e7d --timeout 10m      # or poll: winmux job status b20e7d
✓ Concluído em 41s (212 linhas de output)

PS> winmux job output b20e7d --tail 20        # only what this command printed

PS> winmux send 3fa91c "codex"                # open a TUI in the same session
PS> winmux submit 3fa91c "summarize the failing tests"
PS> winmux status 3fa91c
⏳ busy (1m 12s remaining)
PS> winmux status 3fa91c
⏸ waiting (1. Allow | 2. Deny)
PS> winmux key 3fa91c enter
```

The CLI's own messages are in Portuguese for now. Commands and flags are in English.

## Install

You need Windows 10 1809+ or Windows 11 (for ConPTY), plus OpenSSH Server if you want remote access.

Download `winmux.exe` and `winmux-daemon.exe` from [Releases](../../releases) (`SHA256SUMS` included), or build from source with Go 1.23+:

```bash
GOOS=windows GOARCH=amd64 go build -o dist/winmux.exe ./cmd/winmux
GOOS=windows GOARCH=amd64 go build -o dist/winmux-daemon.exe ./cmd/daemon
```

Start the daemon as the user whose tools you want to drive:

```powershell
Start-Process .\winmux-daemon.exe -WindowStyle Hidden
```

To start it at every logon, create a scheduled task:

```powershell
schtasks /Create /SC ONLOGON /TN winmux /TR "C:\tools\winmux-daemon.exe"
```

## Commands

| Command | What it does |
|---|---|
| `new [--shell powershell\|cmd\|<path>]` | Start a session in ConPTY, in your home directory, and print its ID |
| `list` | Sessions with PID, shell, uptime and directory |
| `send <id> <text>` / `submit <id> <text>` | Type text and press Enter |
| `key <id> <key>` | `esc`, `enter`, `tab`, `ctrl+c`, `ctrl+d`, arrows, `pgup`/`pgdn`… |
| `read <id> [n]` / `logs <id>` | Last *n* lines of output (default 50) / last 500 lines |
| `status <id>` | `idle`, `busy`, `waiting` or `error`, with a detail hint |
| `attach <id>` | Live two-way stream into the session |
| `watch <id> --on-idle <cmd> --on-waiting <cmd>` | Run a callback when the state changes (webhooks, notifications) |
| `reset <id>` | Ctrl+C + Enter, to get out of PowerShell's `>>` continuation prompt |
| `job run\|status\|output\|wait\|list\|kill` | Asynchronous mode for agents (see below) |
| `kill <id>` | End the session |

## How it works

**One reader, many listeners.** Each session has exactly one goroutine reading from ConPTY. It appends the output to a 10,000-line ring buffer and copies the raw bytes to any attached clients. Attaching therefore never takes output away from `read`, `status` or jobs.

**State detection** (`status`, `watch`, jobs) reads the last 30 lines, plus the current unfinished line, because that's where prompts and `(y/n)` questions sit. The rules are checked in order:

1. **busy:** braille or ASCII spinners, progress bars (`[===>  ]`, `█░`), counters like `[3/10]`, or words like *downloading* and *building*
2. **waiting:** trust prompts, `(y/n)`, `press enter`, `? for shortcuts`, password prompts, `--More--`, or PowerShell's `>>`
3. **error:** the last non-empty line looks like `error:`, `fatal:` or `failed`
4. **idle:** none of the above

**Jobs** are for agents that shouldn't block on long commands. `job run` records a line cursor, sends the command and returns a job ID straight away. The job collects only the lines printed after that cursor. It counts as done when the session is idle *and* a shell prompt (`PS C:\…>`) is back on the current line for 1.5 seconds. Without the prompt check, a command that prints nothing, like `Start-Sleep 30`, would look finished at once.

**IPC** is newline-delimited JSON over `\\.\pipe\winmux`. `attach` switches the connection to a raw byte stream after a one-line JSON handshake.

## Security model

winmux runs whatever it receives as the user running the daemon. Treat access to the pipe as access to that account.

- **The pipe is local only, with an explicit access list.** Only SYSTEM, Administrators and the user who started the daemon can connect. Without this, Windows' default pipe permissions would also give *Everyone* and *Anonymous* read access. Connections from other machines over SMB are refused.
- **Remote access goes through SSH.** winmux never opens a network port, so its authentication is whatever your OpenSSH Server enforces.
- **`watch` callbacks** run through `cmd /C` as the daemon's user, which is the same user who was allowed to set them up.
- **Service mode** (`--install-service`) runs as **LocalSystem**. Sessions then run as SYSTEM in session 0, which is usually not what you want: your user's Codex or Gemini install and its credentials aren't there. Use the logon task above unless you specifically need SYSTEM.

## Limitations

- State detection is a heuristic over terminal text. It's tuned for PowerShell, Codex CLI and Gemini CLI. A TUI that never shows a shell prompt will never mark a job as done, so use `status` or `watch` for those.
- `attach` passes raw bytes through without putting the local console into raw mode. It works, but it's not a full terminal emulator.
- Sessions live in the daemon's memory. If you restart the daemon, they're gone.

## Development

The core logic (buffer, state detection, job runner, IPC protocol) has no Windows dependency. It's tested on Linux, with the race detector on, against a mock session. The ConPTY layer is tested on a real Windows runner in CI. A PowerShell session is checked end to end: prompt detection, output capture with an attached client, and a silent `Start-Sleep` job that must not finish early. Then the built daemon and CLI run a job over the real named pipe.

```bash
go test -race ./...                  # any OS
GOOS=windows go vet ./...            # cross-check the Windows code
```

Pushing a `v*` tag builds and publishes the Windows binaries with checksums.

## License

[MIT](LICENSE) © 2026 Christian Conce
