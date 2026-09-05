# WhatsappGo

Self-hosted WhatsApp API manager — create instances, pair via QR code or pairing
code, send text/media/interactive messages, receive real-time events. English UI.

## How it works

- **Your own license server is built in.** Set `LICENSE_MODE=self` and the app
  issues licenses itself: keys + registration emails live in **your own
  database** (`self_licenses`, `self_auth_codes`, `self_reg_tokens`).
  Nothing is sent to any third party — the service works as long as you run it.
- **Manager UI** (English) at `/manager/login`: sign in with your API URL +
  `GLOBAL_API_KEY`, complete the one-time email activation, manage everything
  from the dashboard.
- **API docs (Swagger)** at `/swagger/index.html`.

## Quick deploy (free)

[![Deploy to Render](https://render.com/images/deploy-to-render-button.svg)](https://render.com/deploy)

1. Click **Deploy to Render** (or New → Blueprint with this repo, `render.yaml`).
2. Create a free Postgres (e.g. Neon) and paste its connection string
   (`?sslmode=require`) into `POSTGRES_AUTH_DB` and `POSTGRES_USERS_DB`.
3. Set `GLOBAL_API_KEY` to a long random secret, keep `LICENSE_MODE=self`.
4. Open `https://<your-app>.onrender.com/manager/login` and activate with
   your email. Done — the API is yours.

## Connect a WhatsApp number

Manager → Instances → New Instance → Create → Connect → scan the QR code with
your phone (WhatsApp → Linked devices → Link a device). Then send from the
Messages page or the API:

```bash
curl -X POST https://<your-app>.onrender.com/send/text \
  -H "Content-Type: application/json" -H "apikey: <INSTANCE_TOKEN>" \
  -d '{"number":"8801XXXXXXXXX","text":"Hello from WhatsappGo!"}'
```

## Notes

- Free hosting sleeps when idle (~15 min), which drops WhatsApp sessions;
  re-scan the QR after a sleep, or upgrade the host for 24/7 use.
- `CLIENT_NAME`, `OS_NAME` and the UI brand are "WhatsappGo".
- License: see `LICENSE` (Apache-2.0 based, as received from upstream).
