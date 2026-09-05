# WhatsappGo

Self-hosted WhatsApp API manager in Go — create instances, pair via QR code or
pairing code, send text / media / interactive messages, receive real-time
events. English UI included.

## Self-hosted license — no third party involved

Set `LICENSE_MODE=self` and the app becomes its own license server:

- License keys + registration emails live in **your own database**
  (`self_licenses`, `self_auth_codes`, `self_reg_tokens`).
- The Manager sign-in flow is unchanged: API URL + `GLOBAL_API_KEY`, then a
  one-time email activation on your own `/license-server/register` page.
- Heartbeats and activations loop back to your own public URL.
- Nothing is sent to any third party — the service works as long as you run it.

Unset `LICENSE_MODE` to fall back to the original remote-licensing behavior.

## Quick deploy (free)

[![Deploy to Render](https://render.com/images/deploy-to-render-button.svg)](https://render.com/deploy)

1. Click **Deploy to Render** (this repo contains `render.yaml`), or
   Render Dashboard → New → Blueprint → select this repo.
2. Create a free Postgres (e.g. Neon) and paste its connection string
   (`?sslmode=require`) into `POSTGRES_AUTH_DB` and `POSTGRES_USERS_DB`.
3. Set `GLOBAL_API_KEY` to a long random secret, keep `LICENSE_MODE=self`.
4. Open `https://<your-app>.onrender.com/manager/login`, sign in, activate
   with your email. Done.

`render.yaml` marks secrets as `sync:false` so they are set in the dashboard,
never committed to git.

## Configuration

| Variable | Description | Default |
|---|---|---|
| `SERVER_PORT` | Server port | `8080` |
| `CLIENT_NAME` | Client identifier | `whatsappgo` |
| `GLOBAL_API_KEY` | API authentication key | **Required** |
| `LICENSE_MODE` | `self` = own license server | _(unset = remote)_ |
| `PUBLIC_URL` | Public base URL (auto-detected on Render) | — |
| `POSTGRES_AUTH_DB` | Auth/session database URL | **Required** |
| `POSTGRES_USERS_DB` | Users/instances database URL | **Required** |
| `DATABASE_SAVE_MESSAGES` | Enable message storage | `false` |
| `EVOLUTION_OPERATOR_EMAIL` | Headless auto-activation email (remote mode) | — |
| `WADEBUG` / `LOGTYPE` | WhatsApp debug level / log format | `INFO` / `console` |
| `MINIO_ENABLED` | Media storage via MinIO/S3 | `false` |

## Connect a WhatsApp number

Manager → Instances → New Instance → Create → Connect → scan the QR code with
your phone (WhatsApp → Linked devices → Link a device). Status flips to
Connected. Then send from the Messages page or the API:

```bash
curl -X POST https://<your-app>.onrender.com/send/text \
  -H "Content-Type: application/json" -H "apikey: <INSTANCE_TOKEN>" \
  -d '{"number":"8801XXXXXXXXX","text":"Hello from WhatsappGo!"}'
```

Key endpoints: `POST /instance/create`, `GET /instance/{name}/qrcode`,
`POST /send/text`, `POST /send/media`, `GET /instance/{name}/status`,
`DELETE /instance/delete/{id}`. Full reference with Swagger UI at
`/swagger/index.html`.

## Project structure

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
├── Dockerfile
├── Makefile
└── render.yaml
```

## Notes

- Free hosting sleeps when idle (~15 min), which drops WhatsApp sessions;
  re-scan the QR after a sleep, or upgrade the host for 24/7 use.
- Tech: Go 1.24+, Gin, [whatsmeow](https://github.com/tulir/whatsmeow),
  PostgreSQL, GORM, Docker.
- License: see `LICENSE`.
