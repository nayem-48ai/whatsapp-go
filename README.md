# WhatsappGo

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

Headless servers: after one manual activation, restarts stay licensed
automatically (the key persists in the database).

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
