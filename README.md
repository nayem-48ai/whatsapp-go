# WhatsappGo

<p align="center"><img src="./public/whatsappgo/logo-400.png" width="120" alt="WhatsappGo logo"/></p>

High-performance WhatsApp API in Go — create instances, pair via QR code or
pairing code, send text / media / interactive messages, receive real-time
events. Ships with an English Manager UI and a **self-hosted license server**:
keys + registration emails live in **your own database**, nothing is sent to
any third party.

## Features

- **High performance** — built with Go for minimal resource usage
- **RESTful API** — clean REST endpoints with Swagger docs
- **Real-time events** — WebSocket, Webhook, AMQP/RabbitMQ and NATS support
- **Media support** — images, videos, audio, documents (MinIO/S3 optional)
- **Message storage** — optional PostgreSQL persistence
- **QR code pairing** — built-in QR code generation for device linking
- **Self-hosted license** — `LICENSE_MODE=self` makes the app its own license
  server; login/activation flow stays the same, data stays yours
- **English Manager UI** — dashboard, instances, working send-message page,
  events guide, settings
- **Docker ready** — production-ready Docker configuration

## Quick Start

### Option 1 — Render (free cloud hosting, recommended)

[![Deploy to Render](https://render.com/images/deploy-to-render-button.svg)](https://render.com/deploy)

1. Click **Deploy to Render** (this repo ships `render.yaml`), or
   Render Dashboard → New → Blueprint → select this repo.
2. Create a free Postgres (e.g. Neon) and paste its connection string
   (`?sslmode=require`) into `POSTGRES_AUTH_DB` and `POSTGRES_USERS_DB`
   (`sync:false` in `render.yaml` means secrets are set in the dashboard,
   never committed to git).
3. Set `GLOBAL_API_KEY` to a long random secret, keep `LICENSE_MODE=self`.
4. Open `https://<your-app>.onrender.com/manager/login`, sign in and
   activate with your email. Done.

> Free hosts sleep after ~15 min idle, which drops WhatsApp sessions —
> re-scan the QR after a sleep, or upgrade for 24/7 use.

### Option 2 — Docker

```bash
git clone https://github.com/nayem-48ai/whatsapp-go.git
cd whatsapp-go

# configure
cp .env.example .env
# edit .env: set GLOBAL_API_KEY, POSTGRES_AUTH_DB, POSTGRES_USERS_DB,
# LICENSE_MODE=self, SERVER_PORT=8080

# build and run
make docker-build
docker run -p 8080:8080 --env-file .env whatsapp-go:latest
# or: make docker-run   (maps 4000:4000 — set SERVER_PORT=4000 in .env)
```

### Option 3 — Docker Compose (app + Postgres together)

```bash
cd docker/examples
cp .env.example .env
# edit .env: GLOBAL_API_KEY, LICENSE_MODE=self, CLIENT_NAME=whatsappgo
docker compose up -d
```

The example compose file (`docker/examples/docker-compose.yml`) starts the API
plus a local Postgres and wires `POSTGRES_AUTH_DB` / `POSTGRES_USERS_DB`
to it. A complete env template is at `docker/examples/.env.example`.
Open `http://localhost:4000/manager/login` (or the `SERVER_PORT` you set).

### Option 4 — Local development (Go 1.24+)

```bash
git clone https://github.com/nayem-48ai/whatsapp-go.git
cd whatsapp-go

make setup          # deps + swagger
cp .env.example .env
# edit .env: GLOBAL_API_KEY, POSTGRES_* (or local Postgres),
# LICENSE_MODE=self
make dev
```

Run `make help` to see all available commands.

## Configuration

Create a `.env` file (see `.env.example`):

```bash
# Server
SERVER_PORT=8080
CLIENT_NAME=whatsappgo

# Security (required)
GLOBAL_API_KEY=your-secure-api-key-here

# Self-hosted license (your own license server, no third party)
LICENSE_MODE=self
# PUBLIC_URL=https://your-app.onrender.com   # auto-detected on Render

# Database (required)
POSTGRES_AUTH_DB=postgresql://user:password@host:5432/dbname?sslmode=require
POSTGRES_USERS_DB=postgresql://user:password@host:5432/dbname?sslmode=require
DATABASE_SAVE_MESSAGES=false

# Logging
WADEBUG=INFO
LOGTYPE=console

# Optional
# AMQP_URL=amqp://guest:guest@localhost:5672/
# NATS_URL=nats://localhost:4222
# WEBHOOK_URL=https://your-webhook-url.com/webhook
# MINIO_ENABLED=true
# MINIO_ENDPOINT=localhost:9000
# MINIO_ACCESS_KEY=minioadmin
# MINIO_SECRET_KEY=minioadmin
```

| Variable | Description | Default |
|---|---|---|
| `SERVER_PORT` | Server port | `8080` |
| `CLIENT_NAME` | Client identifier | `whatsappgo` |
| `GLOBAL_API_KEY` | API authentication key | **Required** |
| `LICENSE_MODE` | `self` = built-in license server | _(unset = remote)_ |
| `PUBLIC_URL` | Public base URL for license callbacks | auto on Render |
| `POSTGRES_AUTH_DB` | Auth/session database URL | **Required** |
| `POSTGRES_USERS_DB` | Users/instances database URL | **Required** |
| `DATABASE_SAVE_MESSAGES` | Enable message storage | `false` |
| `WADEBUG` | WhatsApp debug level | `INFO` |

## License Activation (self-hosted)

1. Start the server — API endpoints return `503` until activated.
2. Open the **Manager** at `http://<host>:<port>/manager/login`.
3. Sign in with your API URL and `GLOBAL_API_KEY`.
4. You are redirected to your own activation page
   (`/license-server/register`) — enter the email for this license.
5. The callback activates the instance; the license (`api_key`) is stored in
   your own database (`self_licenses` table) and status becomes `active`.

## Universal license portal (for everyone)

Fresh `whatsapp-go` deployments verify against the public portal by default
— no configuration needed:

- Portal: `https://whatsappgo.tnxbd.top/license-server/register` — branded
  activation with **Continue with Google** (verified Gmail) or email.
- **Leave `LICENSE_MODE` unset** (delete it or leave empty) to use the
  universal portal — this is what gives your users the Google login button
  with zero setup on their side.
- Prefer full autonomy? `LICENSE_MODE=self` turns any deployment (Docker/VPS)
  into its own license server with zero config — but then **email-only**
  registration applies, unless you configure your own Google OAuth client
  (`GOOGLE_CLIENT_ID` + `GOOGLE_CLIENT_SECRET` env vars, with your portal URL
  registered as an authorized redirect URI in Google Cloud Console).
  Never put these secrets in git.
- Operators can point deployments at a different portal with
  `LICENSE_SERVER_URL=https://…`.

Headless servers: after one manual activation, restarts stay licensed
automatically (the key persists in the database).

**Docker / local users:** no extra setup. The license server builds its links
from the URL in your browser and talks to itself over `localhost`, so
activation works the same on `http://localhost:8080`, a LAN IP, or a VPS —
no `PUBLIC_URL` needed (set it only for custom domains / reverse proxies).

## Security & privacy (read this before going public)

WhatsappGo is a public repo meant to run on **your own** infrastructure.
Your data stays yours by design — but understand the model:

| Item | Where it lives | Who can see it |
|---|---|---|
| `GLOBAL_API_KEY` | Your env vars / host dashboard only — never in git | Anyone you share it with (treat it as the master password) |
| License keys + registration emails | Your own Postgres (`self_licenses`, `self_auth_codes`, `self_reg_tokens`) | Only whoever can read your database |
| Activation / heartbeats | Loop back to your own public URL | Nobody else — no telemetry ever leaves your server |
| WhatsApp sessions / messages | Your own Postgres (`instances`, `whatsmeow_*`, optional `messages`) | Same as above |

Protections built in:

- **Registration is owner-only.** Starting a license registration requires
  the deployment's `GLOBAL_API_KEY` (the Manager sends it automatically after
  sign-in). Strangers cannot mint licenses or write emails into your DB.
- **One-time codes.** Activation codes are 192-bit random, bound to one
  instance, expire in 15 minutes, single-use.
- **Signed activation.** `/v1/activate` and `/v1/heartbeat` require an
  HMAC-SHA256 signature made with the license key — stolen instance IDs
  alone are useless.
- **Rate limits.** Public license endpoints are throttled per IP
  (registration pages 30/min, API 120/min).
- **Keep secrets out of git.** `render.yaml` uses `sync:false` for all
  secrets; `.env` is gitignored. Never paste connection strings or keys into
  issues, screenshots, or QR-shared chats.

Recommendations for a public deployment: strong random `GLOBAL_API_KEY`,
separate database per deployment, `DATABASE_SAVE_MESSAGES=false` unless you
need history, and a host with disk encryption for anything beyond testing.

## API Documentation

Swagger UI:

```
http://<host>:<port>/swagger/index.html
```

### Key Endpoints

| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/instance/create` | Create WhatsApp instance (`{"name","token"}`) |
| `GET` | `/instance/{name}/qrcode` | Get QR code for pairing |
| `POST` | `/send/text` | Send text message (`{"number","text"}`, instance token in `apikey` header) |
| `POST` | `/send/media` | Send media message |
| `GET` | `/instance/{name}/status` | Get instance status |
| `DELETE` | `/instance/delete/{id}` | Delete instance |
| `GET` | `/license/status` | License status (public, used as health check) |

Connect a number: Manager → Instances → New Instance → Create → Connect →
scan the QR with your phone (WhatsApp → Linked devices → Link a device).

## Project Structure

```
├── cmd/evolution-go/     # Application entry point
├── pkg/
│   ├── core/            # License engine + self-hosted license server
│   ├── instance/        # Instance management
│   ├── message/         # Message handling
│   ├── sendMessage/     # Message sending
│   ├── routes/          # HTTP routes
│   ├── middleware/      # Auth & validation middleware
│   ├── config/          # Configuration
│   ├── events/          # Event producers (AMQP, NATS, Webhook, WS)
│   └── storage/         # Media storage (MinIO)
├── manager/dist/        # Manager frontend (English UI)
├── docs/                # Swagger documentation
├── docker/examples/     # docker-compose + env templates
├── Dockerfile
├── Makefile
└── render.yaml          # Render Blueprint (free web service)
```

## Tech Stack

| Component | Technology |
|---|---|
| Language | Go 1.24+ |
| HTTP framework | Gin |
| WhatsApp | [whatsmeow](https://github.com/tulir/whatsmeow) |
| Database | PostgreSQL |
| ORM | GORM |
| Message queue | RabbitMQ, NATS |
| Object storage | MinIO/S3 |
| Documentation | Swagger/OpenAPI |
| Container | Docker |

## License

See `LICENSE`.
