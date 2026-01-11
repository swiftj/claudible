# CLAUDE.md

## Project Overview
Claudible is a notification system for Claude Code hooks, written in Go. It acts as a middleware to route Claude's events to various notification providers (iMessage, SMS, HTTP, etc.).
**Core flow**: Hook Event → Stdin → Parse JSON → Smart State Classification (optional LLM) → Route to Providers.

## Build & Development
- **Build**: `make build` (outputs to `build/claudible`)
- **Test**: `make test` (runs all unit/race tests) or `go test -run TestName ./pkg`
- **Lint/Fmt**: `make lint`, `make fmt`
- **Install**: `make install`

## Architecture
- **Entry**: `cmd/claudible/main.go`
- **Config**: `internal/config/` (See `config.example.yaml` for schema)
- **Core Logic**:
  - `internal/hook/`: Parses stdin JSON from Claude Code.
  - `internal/transcript/`: Analyzes JSONL transcripts.
  - `internal/state/`: Determines agent state (Complete/Waiting/Stopped/Idle).
  - `internal/evaluator/`: Optional LLM-based classifier (Anthropic/OpenAI).
  - `internal/router/`: Orchestrates dispatch to providers.
- **Providers** (`internal/providers/`):
  - `imessage/`: macOS-only (AppleScript).
  - `sms/`: Twilio, Vonage, Plivo.
  - `desktop/`: Native OS notifications (macos/linux).
  - `http/`, `pushover/`, `pushbullet/`: Generic web/push providers.

## Key Concepts
- **HookInput**: The contract for data received via stdin.
- **Provider Interface**: All notification backends must implement `Send(ctx, msg)`.
- **State Classification**: Logic to determine if a task is truly finished or just waiting.

## Guidelines
- Follow existing Go idioms and project structure.
- Run `make test` and `make lint` before finishing tasks.
- Ensure cross-platform compatibility where possible (check build tags for OS-specific providers like iMessage).
- Single static binary, no CGO
