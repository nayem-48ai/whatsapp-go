# WhatsappGo — Project Report

Personal self-hosted WhatsApp API. English UI, WhatsappGo brand, own license
server. Nothing depends on any third-party license service.

> 🔑 Secrets (global API key, DB passwords, tokens) are **not** in this file.
> They live in Render env vars + the operator's notes. Never commit them.

## Live endpoints

| What | URL |
|---|---|
| App root / health | `https://whatsappgo-cttc.onrender.com/server/ok` |
| Manager login | `https://whatsappgo-cttc.onrender.com/manager/login` |
| API docs (Swagger) | `https://whatsappgo-cttc.onrender.com/swagger/index.html` |
| License status (public) | `https://whatsappgo-cttc.onrender.com/license/status` |
| Own activation page | `https://whatsappgo-cttc.onrender.com/license-server/register?token=…` (per-session link from `/license/register`) |
| Landing page | `https://whatsappgo.vercel.app` |
| Code | `https://github.com/nayem-48ai/whatsapp-go` (public) |
| Code backup (old Evolution Go) | `https://github.com/nayem-48ai/evolution-go-backup` (private) |

## Login

- **API URL:** `https://whatsappgo-cttc.onrender.com`
- **API key:** value of `GLOBAL_API_KEY` (set at deploy time, stored in Render
  env vars; invent with `openssl rand -hex 16`).
- First login → automatic redirect to the self-hosted activation page →
  enter email → success page → back to Manager, licensed. The license key is
  generated server-side and stored in your own DB (`self_licenses` +
  `runtime_configs`); restarts stay licensed automatically.

## Infrastructure (all free)

| Piece | Where | Notes |
|---|---|---|
| API hosting | Render free web service `whatsappgo` (Singapore, Docker) | Sleeps after ~15 min idle → WhatsApp sessions drop; re-scan QR or upgrade for 24/7 |
| Database | Neon Postgres project `whatsapp-go` (`aws-ap-southeast-1`, PG16) | Permanent; app data + license tables (`self_*`, `runtime_configs`, `instances`, `whatsmeow_*`) |
| Landing page | Vercel project `whatsappgo` → `whatsappgo.vercel.app` | Static, never sleeps; redeploy via Vercel API with `target: production` |
| Code | GitHub `nayem-48ai/whatsapp-go` | `render.yaml` Blueprint included; secrets are `sync:false` (dashboard-only) |
| Brand assets | `public/whatsappgo/` in repo | `logo.svg`, `favicon.svg`, icons, `og-cover.png` (1200×630 social cover) |

## Key endpoints

| Method | Endpoint | Notes |
|---|---|---|
| `POST` | `/instance/create` | `{"name","token"}` + `apikey: GLOBAL_API_KEY` |
| `GET` | `/instance/qr` | QR + pairing code, `apikey: <INSTANCE_TOKEN>` |
| `POST` | `/send/text` | `{"number","text"}`, `apikey: <INSTANCE_TOKEN>` |
| `POST` | `/send/media` | media message |
| `GET` | `/instance/{name}/status` | connection status |
| `DELETE` | `/instance/delete/{id}` | delete instance |

Connect a number: Manager → Instances → New Instance → Create → Connect →
scan QR (WhatsApp → Linked devices → Link a device).

## Connect & send test (curl)

```bash
API=https://whatsappgo-cttc.onrender.com
KEY=<GLOBAL_API_KEY>
curl -X POST $API/instance/create -H 'Content-Type: application/json' \
  -H "apikey: $KEY" -d '{"name":"testwa","token":"<pick-a-token>"}'
curl $API/instance/qr -H "apikey: <pick-a-token>"   # scan the QR
curl -X POST $API/send/text -H 'Content-Type: application/json' \
  -H "apikey: <pick-a-token>" \
  -d '{"number":"8801XXXXXXXXX","text":"Hello from WhatsappGo!"}'
```

## For friends (their own copy)

1. Click **Deploy to Render** in the README (or Blueprint this repo).
2. Attach their own free Postgres, set their own `GLOBAL_API_KEY`,
   keep `LICENSE_MODE=self`.
3. Open their Manager, activate with their email. Independent, no shared fate.
   (Or share one service: one instance per friend, each with its own token —
   but the admin controls everything, and free-tier sleep affects all.)

## What was customized vs upstream

- `pkg/core/selfhost.go` (new): full license server — register/init/page/
  complete/success, code exchange, email auto-activation, HMAC-signed
  activate/heartbeat/deactivate; data in your Postgres.
- `pkg/core/c0.go`: self base-URL override, `/v1/*` + `/license-server/*`
  allowlisted when `LICENSE_MODE=self`, migrates `self_*` tables.
- `cmd/evolution-go/main.go`: tier `whatsapp-go`, mounts self routes.
- Manager (`manager/dist`): full English translation, real Dashboard,
  working Messages sender, Events guide, Settings page, WhatsappGo brand +
  favicon, `Evolution GO Manager` tab title fixed.
- Docs: README/COMMANDS/wiki rebranded, `.env.example` rewritten for the fork,
  Swagger titles rebranded.
- License pages: branded multi-step portal (form → success interstitial →
  auto-continue to Manager).

## History / retired

- Old `evolution-go` deployment (Render service + Neon project
  `wispy-math-16544125` + GitHub repo) was **deleted everywhere** after backup.
- Backup (code + full DB dump + restore guide): private repo
  `nayem-48ai/evolution-go-backup`, `RESTORE.md` inside.
- One leaked Neon password (from an early bad commit) was rotated immediately;
  old password is dead.
