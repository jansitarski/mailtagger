# mailtagger

A lightweight, self-hosted AI-powered Gmail labeler. Polls your Gmail inbox, classifies messages using an LLM, and automatically applies labels.

## Features

- Single binary deployment (no runtime dependencies)
- Multiple LLM providers: OpenAI, Anthropic, Gemini, Ollama
- Multiple Gmail account support
- Customizable classification categories
- Encrypted token storage in SQLite
- Prometheus metrics endpoint
- Docker support

## Requirements

- Go 1.22+ (for building from source)
- Google Cloud project with Gmail API enabled
- OAuth 2.0 credentials (client_secret.json)
- LLM API key (OpenAI, Anthropic, or Gemini) or local Ollama instance

## Installation

### From Source

```bash
git clone https://github.com/jansitarski/mailtagger.git
cd mailtagger
make build
```

The binary will be at `bin/mailtagger`.

### Docker

```bash
docker build -t mailtagger -f deploy/docker/Dockerfile .
```

## Quick Start

### 1. Set Up Google Cloud OAuth

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project or select an existing one
3. Enable the Gmail API
4. Go to **Credentials** > **Create Credentials** > **OAuth client ID**
5. Select **Desktop app** as the application type
6. Download the credentials as `client_secret.json`

### 2. Generate Encryption Key

mailtagger encrypts OAuth tokens at rest. Generate a 32-byte key:

```bash
openssl rand -hex 32
```

Export it as an environment variable:

```bash
export MAILTAGGER_ENCRYPTION_KEY="<your-64-char-hex-key>"
```

### 3. Create Data Directory

```bash
sudo mkdir -p /var/lib/mailtagger
sudo chown $(whoami) /var/lib/mailtagger
```

### 4. Authenticate Gmail Account

```bash
./bin/mailtagger auth \
  --client-secret /path/to/client_secret.json \
  --db /var/lib/mailtagger/state.db
```

This opens a browser for OAuth consent. The account email and encrypted tokens are stored in the database automatically.

For headless environments (no browser), use manual mode:

```bash
./bin/mailtagger auth \
  --client-secret /path/to/client_secret.json \
  --db /var/lib/mailtagger/state.db \
  --manual
```

You can authenticate multiple accounts by running the `auth` command for each email.

### 5. Create Configuration

Copy the example config and customize it:

```bash
cp config.example.yaml /etc/mailtagger/config.yaml
```

Edit the config to set your LLM provider and categories:

```yaml
llm:
  provider: openai
  model: gpt-4-turbo
  api_key: ${OPENAI_API_KEY}
  temperature: 0.1

poll_interval: 5m

store:
  type: sqlite
  path: /var/lib/mailtagger/state.db

http:
  addr: :8080

categories:
  - name: newsletter
    label: AI/newsletter
    description: |
      Marketing emails, newsletters, promotional content.

  - name: receipt
    label: AI/receipt
    description: |
      Purchase confirmations, order receipts, invoices.

  - name: notification
    label: AI/notification
    description: |
      Service notifications, alerts, automated system messages.
```

### 6. Start the Server

```bash
./bin/mailtagger serve --config /etc/mailtagger/config.yaml
```

Or with Docker:

```bash
docker run -d \
  -v /etc/mailtagger:/etc/mailtagger:ro \
  -v /var/lib/mailtagger:/var/lib/mailtagger \
  -e OPENAI_API_KEY \
  -e MAILTAGGER_ENCRYPTION_KEY \
  -p 8080:8080 \
  mailtagger
```

## Commands

### serve

Start the HTTP server and email classification worker.

```bash
mailtagger serve [flags]
```

Flags:
- `-c, --config string` - Path to config file (default `/etc/mailtagger/config.yaml`)
- `--addr string` - HTTP listen address, overrides config (default `:8080`)

### auth

Authenticate a Gmail account via CLI. Tokens are encrypted and stored in the database.

```bash
mailtagger auth [flags]
```

Flags:
- `--client-secret string` - Path to OAuth client_secret.json (required)
- `--db string` - Path to SQLite database (default `/var/lib/mailtagger/state.db`)
- `--encryption-key string` - 32-byte encryption key in hex (or use `MAILTAGGER_ENCRYPTION_KEY` env)
- `--timeout duration` - Timeout for OAuth flow (default `5m`)
- `--manual` - Use manual paste flow instead of local callback server

### reset-cursor

Reset the Gmail history cursor to re-process messages.

```bash
mailtagger reset-cursor --account <id> [flags]
```

Flags:
- `--account string` - Account ID to reset, or `all` (required)
- `--db string` - Path to SQLite database (default `/var/lib/mailtagger/state.db`)

## Configuration

See [config.example.yaml](config.example.yaml) for a fully documented example.

### Environment Variable Expansion

Config values support `${VAR_NAME}` syntax for environment variables:

```yaml
llm:
  api_key: ${OPENAI_API_KEY}
```

### LLM Providers

| Provider | Model Examples | Notes |
|----------|----------------|-------|
| `openai` | `gpt-4`, `gpt-4-turbo`, `gpt-3.5-turbo` | Requires `api_key` |
| `anthropic` | `claude-3-5-sonnet-20241022`, `claude-3-opus-20240229` | Requires `api_key` |
| `gemini` | `gemini-pro`, `gemini-1.5-pro` | Requires `api_key` |
| `ollama` | `llama2`, `mistral` | Set `base_url` to Ollama server |

### HTTP Endpoints

| Endpoint | Description |
|----------|-------------|
| `/healthz` | Health check (returns 200 if healthy) |
| `/metrics` | Prometheus metrics (if `metrics_enabled: true`) |

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                     mailtagger                          │
├─────────────────────────────────────────────────────────┤
│  ┌─────────┐    ┌────────────┐    ┌─────────────────┐  │
│  │  auth   │───▶│  SQLite    │◀───│    pipeline     │  │
│  │ command │    │ (accounts, │    │ (poll, classify │  │
│  └─────────┘    │  tokens,   │    │  label)         │  │
│                 │  history)  │    └────────┬────────┘  │
│                 └────────────┘             │           │
│                                            ▼           │
│  ┌─────────┐                      ┌─────────────────┐  │
│  │  HTTP   │                      │   Gmail API     │  │
│  │ server  │                      └─────────────────┘  │
│  └─────────┘                               │           │
│       │                                    ▼           │
│       │                           ┌─────────────────┐  │
│       │                           │   LLM Provider  │  │
│       │                           └─────────────────┘  │
└───────┼─────────────────────────────────────────────────┘
        │
   /healthz, /metrics
```

## Development

```bash
# Run tests
make test

# Run linter
make lint

# Build and run locally
make run
```

## License

Apache License 2.0. See [LICENSE](LICENSE).
