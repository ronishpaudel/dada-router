# CTX: dada-router Architecture & Technical Context

This document maintains the architectural reference, core invariants, error contracts, and design specifications for **dada-router**.

---

## 1. Executive Summary

`dada-router` (`dada`) is a single-binary Go CLI that provides a unified, continuous terminal session across five major AI providers:
1. **OpenAI** (`api.openai.com`)
2. **Anthropic** (`api.anthropic.com`)
3. **Google Gemini** (`generativelanguage.googleapis.com`)
4. **xAI (Grok)** (`api.x.ai`)
5. **DeepSeek** (`api.deepseek.com`)

### Core Value Propositions
- **Bring Your Own Keys (BYOK)**: No telemetry, no hosted backend, no user accounts, no proxy servers, no consumer OAuth or subscription pooling.
- **Mid-Conversation Context Handoff**: Seamlessly switch providers mid-chat (`/switch <provider>`) carrying full in-memory conversation history translated into the destination provider's wire format.
- **Deterministic Multi-Provider Routes**: Route fallbacks execute automatically on rate limits (429) or server errors (5xx) *before* the first streamed token.
- **Local Quota & Spend Guards**: Track token and request windows, parse `retry-after`, enforce daily spending ceilings at local midnight, and render local quota bars with reset time stamps.
- **Rigorous Key Protection**: Primary storage in the OS keychain; encrypted-file fallback using Argon2id and XChaCha20-Poly1305; mandatory redaction of keys and tokens across errors and logs.

---

## 2. Naming & Filesystem Conventions

- **Project Name**: `dada-router`
- **Go Module**: `github.com/yourname/dada-router`
- **Binary Name**: `dada` (output path: `bin/dada`)
- **Keyring Service Name**: `dada-router`
- **Config Directory**: `~/.config/dada-router/` (overridable via `DADA_ROUTER_CONFIG_DIR`)
- **Settings File**: `~/.config/dada-router/settings.json` (requires `"version": 1`, permissions checked to refuse group/world-writable files)
- **Log File**: `~/.config/dada-router/logs/dada-router.log` (file mode `0600`, directory mode `0700`)
- **Environment Variable Prefix**: `DADA_ROUTER_`
  - `DADA_ROUTER_CONFIG_DIR`: Custom configuration directory
  - `DADA_ROUTER_<PROVIDER>_KEY`: CI-only key injection (warns if used interactively, never echoed)
  - `DADA_ROUTER_ALLOW_CUSTOM_ENDPOINT`: Set to `1` to permit endpoint override (warns on every invocation)

---

## 3. Command Surface

### 3.1 CLI Commands (Shell)
- `dada` (or `dada chat`): Start interactive chat session.
  - `--route <name>`: Start chat session on a specified route.
  - `--insecure-file-store`: Enable encrypted-file fallback for headless Linux (prints warning).
- `dada setup`: Interactive wizard to set or update API keys; validates each with a 1-token request before storing.
- `dada usage`: Standalone output displaying per-provider usage and quota bars outside chat.
- `dada doctor`: Validate stored keys, verify configured models still exist, report pinned API versions, warn on defaults older than 60 days.
- `dada route list | show <name> | set <name> ...`: Inspect and manage routing policies.
- `dada keys remove <provider>`: Remove stored provider credentials from keychain/keystore.
- `dada reset [--yes]`: Securely wipe keys, cache, logs, and settings after user confirmation.
- `dada logs`: Print redacted execution logs (when logging is enabled).

### 3.2 In-Chat Slash Commands
Inside an active chat session, any line starting with `/` is evaluated as a command:
- `/switch <provider>`: Switch active provider; full conversation history is re-serialized and carried over.
- `/model <provider> <name>`: Change active model for a given provider.
- `/route <name>`: Switch the active route.
- `/usage`: Render quota and usage bars with local reset times.
- `/clear`: Clear in-memory conversation history.
- `/help`: Display available commands.
- `/quit`: Exit chat cleanly with status code `0`.

Prompt format: `[<provider> · <model>] >` (e.g., `[gemini · flash] >`).

---

## 4. Provider Specifications & Endpoints

### 4.1 Endpoints (Hardcoded Official Hosts)
| Provider | Hostname | Authentication Header | Notes |
| :--- | :--- | :--- | :--- |
| **OpenAI** | `api.openai.com` | `Authorization: Bearer <key>` | SSE streaming |
| **Anthropic** | `api.anthropic.com` | `x-api-key: <key>`, `anthropic-version: <version>` | SSE streaming |
| **Gemini** | `generativelanguage.googleapis.com` | `x-goog-api-key: <key>` | Headers only (never in URL query params) |
| **xAI** | `api.x.ai` | `Authorization: Bearer <key>` | OpenAI-compatible SSE |
| **DeepSeek** | `api.deepseek.com` | `Authorization: Bearer <key>` | OpenAI-compatible SSE |

*Security Rule*: Keys MUST never appear in query parameters or URLs. Base URL override requires `DADA_ROUTER_ALLOW_CUSTOM_ENDPOINT=1` and prints an explicit warning on every network request.

### 4.2 Streaming & Fallback Mechanics
- Streaming uses Server-Sent Events (SSE).
- **Fallback Rule**: Route fallback is permitted **only before the first streamed token** has been emitted to stdout.
- If a stream disconnects or errors *after* output begins:
  - Keep the partial reply.
  - Mark it `partial: true`.
  - Output an inline error marker.
  - Do NOT trigger fallback (avoids duplicate or disjoint answers).

---

## 5. Context Handoff & Token Budgeting

1. **Re-serialization**: When `/switch <provider>` occurs, the in-memory message history is converted directly into the target provider's expected wire structure.
2. **Unsupported Features**: Provider-specific flags (e.g. `cache_control`, tool schemas) that cannot be translated are dropped with a clear one-line notice.
3. **Token Estimation Formula**:
   $$\text{Estimated Tokens} = \left(\frac{\text{len(text)}}{4}\right) \times 1.10$$
4. **Budget Management**:
   - If history exceeds target model's input token limit, oldest non-system turns are trimmed sequentially until budget fits.
   - An informative notice reports dropped turns.
   - History is strictly ephemeral and in-memory; nothing is persisted to disk on exit.

---

## 6. Routing, Quotas & Error Handling

### 6.1 Route Configuration
A route consists of:
- `ordered_providers`: Prioritized list of providers.
- `model_per_provider`: Specific model name assigned to each provider in the route.
- `allowed_providers`: Whitelist of providers permitted on this route. Providers outside this list are never called, even during fallback.
- `fallback_policy`: Criteria governing fallback conditions.
- `timeout`: Optional per-route timeout override (default: connect 5s, first byte 30s, total 120s).
- `cache`: Boolean flag for response caching (off by default).

A default route named `dada` exists out of the box. Preference rules are deterministic and transparent (e.g., short queries to cheaper models, code to high-reasoning models). No opaque/learned routing.

### 6.2 Strict Error Mapping
| HTTP Status | Internal Error | Action / Fallback Behavior |
| :--- | :--- | :--- |
| **401, 403** | `ErrAuth` | No retry. Skip provider for remainder of session. |
| **404** (model) | `ErrModelNotFound` | Fall back to next provider. Display notice recommending `dada doctor`. |
| **400** (context length) | `ErrContextTooLarge` | Re-run handoff once at 70% of budget. If it fails again, return `ErrContextTooLarge`. |
| **429** | `ErrRateLimited` | Parse `retry-after` header. No retry on same provider. Fall back to next provider. |
| **5xx** | `ErrServer` | Single retry with 250–750 ms random jitter. If still failing, fall back to next provider. |
| **Timeout / Deadline** | `ErrServer` | Fall back to next provider. |
| **Other 4xx** | Standard error | Wrapped with `%w`. No fallback. |

*Error Sanitization*: All error response bodies are truncated to 200 characters and filtered through `redact.go` before display.

### 6.3 Quotas & Spend Guard
- Window tracking for requests and tokens per provider.
- `settings.json` provides user-specified rate caps, defaulting to `defaults.go` and dynamically reconciled when headers return live limits.
- Reset times are formatted in the user's local timezone with timezone abbreviation.
- **Spend Guard**: Daily token ceiling is validated prior to every network call. Resets at local midnight. If exceeded, request is blocked before dialing.

---

## 7. Security, Secrets & Redaction

1. **Secret Safety**:
   - API keys are never written to logs, errors, console dumps, or panics.
   - Errors are wrapped with `%w` without echoing authorization headers or tokens.
   - Key byte slices in memory are zeroed on a best-effort basis upon completion of operations.
2. **Redaction Engine (`redact.go`)**:
   - Regex patterns redact: `sk-[a-zA-Z0-9_-]{20,}`, `sk-ant-[a-zA-Z0-9_-]{20,}`, `AIza[0-9A-Za-z-_]{35}`, `xai-[a-zA-Z0-9_-]{20,}`, generic bearer tokens, JWT tokens (`eyJ...`), and PEM private key blocks (`-----BEGIN ... PRIVATE KEY-----`).
3. **Encrypted-File Fallback (`--insecure-file-store`)**:
   - Used only on headless Linux lacking D-Bus / SecretService.
   - Key derivation: Argon2id.
   - Cipher: XChaCha20-Poly1305 with fresh cryptographically secure random nonce per write.
   - Atomic write pattern (write temporary file, fsync, atomic rename). File permissions `0600`.
   - Passphrase prompted every run via hidden terminal input; never saved to disk.
4. **Response Cache**:
   - Disabled by default.
   - Cache key: $\text{SHA256}(\text{provider} + \text{model} + \text{route} + \text{normalized\_request})$.
   - TTL: 24 hours. Wiped by `dada reset`.

---

## 8. Process Signals, Output & Exit Codes

### Exit Codes
- `0`: Success (or `/quit`, Ctrl-C at prompt).
- `1`: General application error.
- `2`: Command usage or argument error.
- `3`: All providers in route unavailable or exhausted.

### Signal Handling
- `SIGINT` (Ctrl-C) during streaming: Cancel context, retain partial output, return to REPL prompt.
- `SIGINT` (Ctrl-C) at REPL prompt: Graceful exit with code `0`.
- `SIGINT` (Ctrl-C) during `setup`: Abort immediately without persisting keys.

### Terminal Output
- ANSI/OSC escape codes stripped from all LLM outputs.
- UI styling and escapes handled exclusively through `lipgloss`.
- Respects `NO_COLOR` environment variable.
- Non-TTY stdout executes one-shot mode without REPL.
