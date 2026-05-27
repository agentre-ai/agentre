# agentred

Headless agent compute daemon for Agentre — a stateless executor that runs
claude-code / codex subprocesses on behalf of remote desktops over a
JSON-RPC over WebSocket control API on the local network.

Spec: `docs/superpowers/specs/2026-05-21-agentred-mvp-design.md`

## Quickstart

```bash
# build the binary
go build -o agentred ./cmd/agentred

# in one shell — start the daemon
./agentred run

# in another shell on the same machine — mint a pairing code
./agentred pair
# → Pairing code: ABC2DE
#   Expires in 300 seconds.
#   On desktop, use any of:
#     ws://192.168.1.100:7456/rpc

# anywhere — inspect status
./agentred status
# → Daemon running, pid 12345
#   Listening on:
#     ws://192.168.1.100:7456/rpc
#   Paired devices: 0
#   Active sessions: 0
#   LLM providers: 0
```

## CLI subcommands

| Command | Purpose |
|---|---|
| `agentred run [flags]` | Boot the daemon (foreground; SIGINT/SIGTERM to stop). |
| `agentred status` | Print daemon state via the local unix socket. |
| `agentred pair` | Mint a one-shot pairing code + advertise listen URLs. |
| `agentred llm list` | List LLM providers without printing raw API keys. |
| `agentred llm add --key=<uuid> --name=<name> --type=<type> --api-key=<key>` | Add or update an LLM provider. |
| `agentred llm remove --key=<uuid>` | Delete an LLM provider. |
| `agentred claudecode <args...>` | Internal: claudecode hook passthrough used by spawned subprocesses. |

`agentred run` flags:
- `--host HOST` (default `0.0.0.0`) — LAN listen address
- `--port PORT` (default `7456`) — LAN listen port
- `--tls-cert PATH` — PEM certificate path; enables `wss://` (must pair with `--tls-key`)
- `--tls-key PATH` — PEM private key path

## Encryption (optional)

By default agentred serves plain `ws://`. To enable `wss://`, provide a cert/key
pair. The recommended way to generate a locally trusted cert is `mkcert`:

```bash
brew install mkcert     # macOS; or use your platform's installer
mkcert -install
mkcert agentred.local 192.168.1.100

./agentred run \
  --tls-cert agentred.local+1.pem \
  --tls-key  agentred.local+1-key.pem
# → serves wss://192.168.1.100:7456/rpc
```

On the desktop side, the user picks one of four TLS trust modes (spec §4.7):
- `default` — OS trust store (works for mkcert-installed CAs)
- `pin-cert` — pin the daemon's leaf cert
- `ca-bundle` — supply a custom CA bundle (corporate PKI)
- `skip-verify` — debug only; bypasses verification

## Storage layout

agentred uses its own `AppDataDir` (separate from agentre desktop):

| Platform | Path |
|---|---|
| macOS | `~/Library/Application Support/agentred/` |
| Linux | `~/.config/agentred/` |
| Windows | `%LOCALAPPDATA%\agentred\` |

Inside:

```
<AppDataDir>/
  state.json         — daemon runtime state (UUID, listen prefs, paired peers, LLM providers)
  agentred.sock      — unix socket for local CLI ↔ daemon IPC
  logs/              — (reserved; structured logging not wired in MVP)
```

LLM API keys are stored directly in `state.json`. `agentred llm list` only
prints masked key tails and never returns raw API keys.

Override the data dir for testing or ops with `AGENTRED_DATA_DIR=/tmp/...`.

## Architecture

- Single binary, no SQLite, no embedded frontend — purely a control daemon.
- Transport: JSON-RPC 2.0 over WebSocket with subprotocol `agentred-jsonrpc.v1`.
- Pairing: 6-char base32 code (TTL 5 min, one-shot, per-IP rate-limited) + 256-bit
  device token + TOFU daemon fingerprint pinning.
- LLM gateway: reuses `internal/pkg/httpgateway` to mint per-call tokens and forward
  Anthropic / OpenAI requests with keys loaded from daemon state.
- Subprocess: dispatches to `internal/pkg/agentruntime`'s BackendRunner registry
  (claudecode / codex backends; builtin not supported).

For protocol-level detail, method table, error codes, and TLS trust contract,
read the design spec at `docs/superpowers/specs/2026-05-21-agentred-mvp-design.md`.
