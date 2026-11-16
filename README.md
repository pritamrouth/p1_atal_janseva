# Ataljanseva WhatsApp Chatbot (Meta Cloud API)

Citizen-service chatbot for WhatsApp, sent through the **Meta WhatsApp Cloud API**
(no Plivo). Backend architecture (Redis sessions, Postgres, worker pool,
inactivity monitor) follows the original `atal_janseva` backend, with the
conversation flow defined in `flow.txt`.

## Conversation flow

```
Any first message
        ↓
Preselected leader's e-Office main menu       ← leader photo + 9 options + "अधिक पर्याय" (Marathi)
        ├── 1  Raise complaint      → text details or voice note → POST /api/grievances → Ticket ID
        ├── 2  Track complaint      → Ticket ID → GET track/ticket → in progress / resolved / invalid
        ├── 3  Schedule meeting     → name/date/reason → free slots → POST book → confirmation
        ├── 4–9 Info replies        → same website redirect (ataljanseva.in) + closing note
        └── अधिक पर्याय             → reveals option 10 (NariShakti), which sends the same redirect
```

The language picker, PIN-code and leader-selection steps are skipped. The
leader is chosen at deployment time with `LEADER_ID` / `LEADER_SLUG` and read
from the `political_users` table. Every citizen lands directly in that leader's
e-Office in **Marathi**.

- Marathi is the default and only language in the live flow. English and Hindi
  strings remain in `i18n.go` for older in-flight sessions.
- Complaints, tracking and appointments use the public **AtalJanseva REST API**
  (`ATALJANSEVA_API_BASE_URL`, no auth).
- Options **4–10** all send the same static message pointing citizens to
  `ataljanseva.in`, followed by the closing thank-you line.
- Voice complaints are downloaded from Meta, stored in S3, and submitted to
  `POST /api/grievances` as `audio_url`. Text complaints work unchanged.
- The main menu is one WhatsApp list with **9 options + “अधिक पर्याय”**
  (Meta caps the **total** row count of a list message at 10, API error
  `#131009`). Tapping the last row reveals NariShakti (option 10). The former
  options 11–13 (Swadeshi, Youth Engagement, Join Our Team) have been removed.
- Typing `reset` at any point restarts the conversation.
- Sessions expire silently after 90 s of inactivity; sessions that reached the
  main menu are persisted to `citizen_ivr` via `upsert_citizen_ivr`. Because no
  PIN is collected, `citizen_ivr.pincode` stays empty.

## Prerequisites

- Go ≥ 1.22, PostgreSQL, Redis
- Meta developer account with a WhatsApp Business Account
- Public HTTPS URL for the webhook (ngrok for local dev)

## Setup

```bash
cp .env.example .env        # fill in WA_* and DATABASE_URL
go mod tidy
go run ./cmd/server
```

Then expose the webhook:

```bash
ngrok http 8080
```

In the Meta developer console set:

- **Callback URL**: `https://xxxx.ngrok.io/webhook`
- **Verify token**: same value as `WA_VERIFY_TOKEN`
- Subscribe to the **messages** webhook field

## Environment variables

Variable names are identical to the original `atal_janseva` backend, so the
same `.env` works (Plivo variables are simply ignored):

| Variable | Default | Purpose |
|---|---|---|
| `WA_PHONE_NUMBER_ID` | – | **required** – Meta phone number ID |
| `WA_ACCESS_TOKEN` | – | **required** – Meta system/user access token |
| `WA_VERIFY_TOKEN` | – | **required** – webhook verify token |
| `WA_MESSAGING_ACCOUNT_ID` | – | optional – Messaging account (formerly WABA) ID for billing; set when the number has multiple Messaging accounts |
| `DATABASE_URL` | – | **required** – PostgreSQL DSN |
| `DB_MAX_OPEN_CONNS` / `DB_MAX_IDLE_CONNS` | 25 / 10 | pool sizes |
| `REDIS_ADDR` / `REDIS_PASSWORD` / `REDIS_DB` | `localhost:6379` / "" / 0 | session store |
| `WORKER_COUNT` / `QUEUE_DEPTH` | 50 / 2000 | inbound message pool |
| `ENABLE_INACTIVITY_MONITOR` | `true` | 90 s idle session expiry |
| `ATALJANSEVA_API_BASE_URL` | `https://api.ataljanseva.in` | AtalJanseva REST API (local: `http://localhost:3000`) |
| `ATALJANSEVA_API_TIMEOUT` | 10 | per-request API timeout (seconds) |
| `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` | – | S3 credentials for voice-note uploads |
| `REGION` | – | S3 region (aliases: `S3_REGION`, `AWS_REGION`) |
| `YOUR_BUCKET_NAME` | – | S3 bucket for complaint audio (alias: `S3_BUCKET`) |
| `LEADER_ID` | – | optional – preselected leader UUID (takes precedence) |
| `LEADER_SLUG` | `meghnabordikar` | preselected leader slug in `political_users` |
| `PORT` / `ENV` | 8080 / development | HTTP server / JSON logs in production |
| `MIGRATIONS_DIR` | `migrations` | where `*.sql` migrations live |

## Database

**Pre-existing** (same as the original backend, created outside this repo):

- `political_users` – preselected leader row, resolved by `LEADER_ID` or
  `LEADER_SLUG` (`is_active = true`)
- `upsert_citizen_ivr(phone, lang, pincode, ward, nagarsevak_id)` – citizen persistence

`get_nagarsevaks_by_pincode(TEXT)` and `get_nagarsevak_by_id(TEXT)` are no
longer called by the chatbot.

**Migrations** (applied automatically at startup, tracked in
`schema_migrations`): `migrations/002_drop_complaints_and_meetings.sql` drops
the retired local `complaints` / `meetings` tables — that data now lives in
the AtalJanseva API.

## AtalJanseva API

Complaints, tracking and appointments (menu options 1–3) call the public REST
API documented in `data/api.txt`:

| Menu option | Endpoint |
|---|---|
| 1 Raise complaint | `POST /api/grievances` → ticketId (`TKT…`); voice notes also send `audio_url` |
| 2 Track complaint | `GET /api/grievances/track/ticket/{ticketId}` |
| 3 Schedule meeting | `GET /api/schedule/appointments/slots` → free slots, then `POST /api/schedule/appointments/book` |

Options 4–10 do not call the API; they all send the same static website
redirect message plus the closing thank-you line.

## Endpoints

| Route | Purpose |
|---|---|
| `GET/POST /webhook` | Meta verification + inbound messages |
| `GET /health` | `{"status":"ok","queue_len":…,"queue_cap":…,"dropped_messages":…}` |

## Tests

```bash
go test ./...
go vet ./...
```

## Docker

```bash
docker build -t ataljanseva-chatbot:latest .
docker run -d --env-file .env -p 8080:8080 --name ataljanseva-chatbot ataljanseva-chatbot:latest
```

The image copies `migrations/` to `/migrations` and sets `MIGRATIONS_DIR`,
so migrations run inside the container.
