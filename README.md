# dada-router

A CLI (`dada`) providing a unified terminal session for OpenAI, Anthropic, Gemini, xAI (Grok), and DeepSeek using your own API keys.

## Key Features

- **Multi-Provider Session**: Seamlessly interact with OpenAI, Anthropic, Gemini, xAI, and DeepSeek in one continuous terminal REPL.
- **Mid-Conversation Context Handoff**: Switch providers mid-stream (`/switch <provider>`) while carrying full conversation context across models without re-pasting.
- **Quota & Usage Gauges**: Real-time per-provider token/request usage bars with reset windows and spend guard limits.
- **Deterministic Routing & Fallback**: Automatic route progression when a provider fails or encounters rate limits, before streamed output begins.
- **Privacy & Security First**:
  - No backend, no consumer OAuth, no account pooling, zero telemetry.
  - Keys stored in system keychain (or optional argon2id + XChaCha20-Poly1305 encrypted file store).
  - Redaction of API keys and bearer tokens across errors and scrubbed logs.

## Quick Start

### Installation

```bash
# Build the binary
go build -o bin/dada ./cmd/dada

# Optional: alias or symlink to your PATH
alias dada="$(pwd)/bin/dada"
```

### Setup

Add or validate your provider keys:

```bash
dada setup
```

### Chat

Start a chat session:

```bash
dada
# or specify a custom route
dada --route coding
```

### In-Chat Commands

Within the terminal chat prompt:

- `/switch <provider>`: Switch provider; carries full conversation context.
- `/model <provider> <name>`: Switch active model for a provider.
- `/route <name>`: Switch the active route.
- `/usage`: Display quota and usage bars.
- `/clear`: Clear the in-memory conversation.
- `/help`: Display in-chat command help.
- `/quit`: Exit the session.

## CLI Commands

- `dada` (or `dada chat`): Open chat session.
- `dada setup`: Interactive key management and 1-token validation.
- `dada usage`: Standalone quota and usage reporting.
- `dada doctor`: Healthcheck keys, verify active models, and check deprecation dates.
- `dada route [list|show|set]`: Manage routes.
- `dada keys remove <provider>`: Remove stored provider credentials.
- `dada reset [--yes]`: Wipe keys, cache, logs, and settings.
- `dada logs`: View redacted execution logs.

## Status

Phase 0: Project foundation initialized.
