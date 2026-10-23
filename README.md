# Ataljanseva WhatsApp Chatbot (Meta Cloud API)

Citizen-service chatbot for WhatsApp, sent through the **Meta WhatsApp Cloud API**
(no Plivo). Backend architecture (Redis sessions, Postgres, worker pool,
inactivity monitor) follows the original `atal_janseva` backend, with the
conversation flow defined in `flow.txt`.

## Conversation flow

```
Language picker (Marathi / English / Hindi)   ← hero banner + 3 reply buttons
        ↓
6-digit PIN code                              ← hero banner image prompt
        ↓
Leader list for the PIN code                  ← WhatsApp list ("Select Leader")
        ↓
Leader e-Office main menu                     ← leader photo + 13-option list
        ├── 1  Raise complaint      → free-text details → POST /api/grievances → Ticket ID
        ├── 2  Track complaint      → Ticket ID → GET track/ticket → in progress / resolved / invalid
        ├── 3  Schedule meeting     → name/date/reason → free slots → POST book → confirmation
        ├── 4–9, 12 Info replies    → live AtalJanseva API listings, static text as fallback
        └── 10, 11, 13 Info replies → localised text + closing note, menu re-shown
```

- All texts are localised in **English / Marathi / Hindi**.
- Complaints, tracking, appointments and the info listings come from the
  public **AtalJanseva REST API** (`ATALJANSEVA_API_BASE_URL`, no auth).
  When the API fails or returns nothing, options 4–9 and 12 fall back to
  the static `flow.txt` text.
- The menu is ONE WhatsApp list with options **1–9 plus a "More Options
  (10-13)" row** — Meta caps the **total** row count of a list message at 10
  (API error `#131009`), not 10 per section. Tapping "More Options" reveals
  options 10–13 in a second list.
- Typing `reset` at any point restarts the conversation.
- Sessions expire silently after 90 s of inactivity; completed sessions
  (PIN + leader) are persisted to `citizen_ivr` via `upsert_citizen_ivr`.

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
| `DATABASE_URL` | – | **required** – PostgreSQL DSN |
| `DB_MAX_OPEN_CONNS` / `DB_MAX_IDLE_CONNS` | 25 / 10 | pool sizes |
| `REDIS_ADDR` / `REDIS_PASSWORD` / `REDIS_DB` | `localhost:6379` / "" / 0 | session store |
| `WORKER_COUNT` / `QUEUE_DEPTH` | 50 / 2000 | inbound message pool |
| `ENABLE_INACTIVITY_MONITOR` | `true` | 90 s idle session expiry |
| `ATALJANSEVA_API_BASE_URL` | `https://api.ataljanseva.in` | AtalJanseva REST API (local: `http://localhost:3000`) |
| `ATALJANSEVA_API_TIMEOUT` | 10 | per-request API timeout (seconds) |
| `PORT` / `ENV` | 8080 / development | HTTP server / JSON logs in production |
| `MIGRATIONS_DIR` | `migrations` | where `*.sql` migrations live |

## Database

**Pre-existing** (same as the original backend, created outside this repo):

- `get_nagarsevaks_by_pincode(TEXT)` – location + leaders for a PIN code
- `get_nagarsevak_by_id(TEXT)` – single leader
- `upsert_citizen_ivr(phone, lang, pincode, ward, nagarsevak_id)` – citizen persistence

**Migrations** (applied automatically at startup, tracked in
`schema_migrations`): `migrations/002_drop_complaints_and_meetings.sql` drops
the retired local `complaints` / `meetings` tables — that data now lives in
the AtalJanseva API.

## AtalJanseva API

Grievances, tracking, appointments and info listings (menu options 1–9, 12)
call the public REST API documented in `data/api.txt`:

| Menu option | Endpoint |
|---|---|
| 1 Raise complaint | `POST /api/grievances` → ticketId (`TKT…`) |
| 2 Track complaint | `GET /api/grievances/track/ticket/{ticketId}` |
| 3 Schedule meeting | `GET /api/schedule/appointments/slots` → free slots, then `POST /api/schedule/appointments/book` |
| 4 About leader | `GET /api/political-users/slug/{slug}` |
| 5 Transparency board | `GET /api/grievances/public/all?status=Resolved` |
| 6 Work reports | `GET /api/work-reports/public` |
| 7 Events | `GET /api/events/public` |
| 8 Schemes | `GET /api/programs/public` |
| 9 JanSeva Connect | `GET /api/local-services/public` |
| 12 Youth campaigns | `GET /api/campaigns/public` |

Options 10 (NariShakti), 11 (Swadeshi) and 13 (Join Our Team) have no
matching endpoint and send static localised text. Listing responses are
parsed defensively (up to 5 items); on any API error the static `flow.txt`
text is sent instead.

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
