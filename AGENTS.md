# AGENTS.md: Developer & Agent Operating Instructions

This document governs the workflow, design rules, constraints, and operational guidelines for all AI agents and contributors working on **dada-router**.

---

## 1. Non-Negotiable Invariants

1. **Zero External Backend & Zero Telemetry**:
   - `dada-router` must remain a 100% client-side CLI.
   - Do NOT introduce any telemetry, metrics collector, phone-home code, crash reporting service, or remote logging.
   - No proxying of requests through intermediary servers.

2. **Official Endpoints Only**:
   - Hardcoded hostnames:
     - `api.openai.com`
     - `api.anthropic.com`
     - `generativelanguage.googleapis.com`
     - `api.x.ai`
     - `api.deepseek.com`
   - Custom endpoints are strictly gated behind `DADA_ROUTER_ALLOW_CUSTOM_ENDPOINT=1` and must print a clear warning on every use. Never call any other host.

3. **No Key Leaks**:
   - Keys must never appear in logs, error strings, panics, or HTTP dumps.
   - All errors must be wrapped using `%w`.
   - All user/provider output and error messages must pass through `redact.go`.
   - Never print raw request headers.
   - Gemini API keys must always be passed in the `x-goog-api-key` header, never in the URL query parameters.

4. **Allowed Dependencies Whitelist**:
   Only the following external modules are permitted without prior human approval:
   - `github.com/spf13/cobra` (CLI structure)
   - `github.com/zalando/go-keyring` (System keychain integration)
   - `github.com/charmbracelet/lipgloss` (Terminal rendering & styling)
   - `github.com/stretchr/testify` (Unit testing assertions)
   - `github.com/jarcoal/httpmock` (HTTP mock testing)
   - `golang.org/x/crypto` (Argon2id, XChaCha20-Poly1305 for encrypted-file fallback)
   - `golang.org/x/term` (Hidden password/key input)
   
   *Do NOT add any other third-party dependencies without explicit user consent. Prefer standard library (`net/http`, `encoding/json`, `context`, etc.).*

---

## 2. Strict Error Handling & Fallback Contracts

Agents must implement the error mapping matrix exactly:
- `401`, `403` $\rightarrow$ Return `ErrAuth`. Do not retry. Skip provider for session.
- `404` (model not found) $\rightarrow$ Return `ErrModelNotFound`. Fall back to next provider in route. Suggest running `dada doctor`.
- `400` (context length exceeded) $\rightarrow$ Retry handoff once at 70% of token budget. If failing again $\rightarrow$ return `ErrContextTooLarge`.
- `429` (rate limited) $\rightarrow$ Return `ErrRateLimited`. Read `Retry-After` header. Do not retry on same provider; fall back to next provider in route.
- `5xx` / Timeout $\rightarrow$ Return `ErrServer`. Perform exactly one retry with 250–750 ms jitter. If still failing, fall back.
- Other `4xx` $\rightarrow$ Return `%w` wrapped error. Do not fall back.
- Stream fallback rule: Fallback is strictly disallowed once the first byte/token has been rendered to stdout. If stream breaks mid-generation, preserve partial output, mark `partial: true`, and display an error marker.

---

## 3. Security & Storage Standards

- **Settings Permissions**: `~/.config/dada-router/settings.json` must be checked. If group- or world-writable, execution must be refused.
- **Log Permissions**: Log directory must be created with `0700` and `dada-router.log` with `0600`.
- **Encrypted Fallback**: Argon2id for KDF, XChaCha20-Poly1305 for AEAD. Always generate a fresh cryptographic nonce per write. Write to a temporary file in the same directory, fsync, and atomic rename.
- **Panic Handler**: Recover at top level (`cmd/dada/main.go`), display a scrubbed user-friendly error, and write the redacted stack trace to the log only if logging is enabled.

---

## 4. Phase Roadmap & Execution Protocols

Work must be performed phase-by-phase. Complete each phase, ensure tests and linters pass, and stop for user review before advancing:
- **Phase 0: Foundation** (Current): Git repository, `.gitignore`, `LICENSE`, `README.md`, `go.mod`, `golangci.yml`, `.github/workflows/ci.yml`, `CTX.md`, `AGENTS.md`.
- **Phase 1: Configuration & Secret Store**: Config loading, validation, system keyring adapter, encrypted-file fallback, redaction engine (`redact.go`), CLI commands (`setup`, `reset`, `keys`).
- **Phase 2: Provider Adapters & Streaming**: HTTP client, version pinning (`versions.go`), OpenAI, Anthropic, Gemini, xAI, DeepSeek adapters, SSE streaming, 1-token probe.
- **Phase 3: Context Handoff & Token Budgeting**: In-memory message store, serializer per provider, token estimator, turn dropping on budget overflow.
- **Phase 4: Routing, Fallback & Quotas**: Route parser, spend guard (midnight local reset), quota tracking, retry-after handler, deterministic fallback.
- **Phase 5: Interactive Terminal Chat & Doctor**: Lipgloss UI, REPL, slash commands (`/switch`, `/model`, `/route`, `/usage`, `/clear`), signal handling (`SIGINT`), `dada doctor`.

---

## 5. Testing & Quality Standards

- Always run `go vet ./...` and `go test -race ./...`.
- All linters in `golangci.yml` (`govet`, `errcheck`, `staticcheck`, `gosec`, `ineffassign`, `unused`) must pass with zero warnings.
- Keep fixtures in `testdata/<provider>/<api-version>/`. Never overwrite existing version fixtures; add new versions side-by-side to track provider drift.
