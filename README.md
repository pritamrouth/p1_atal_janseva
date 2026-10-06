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
        ├── 1  Raise complaint      → free-text details → Ticket ID
        ├── 2  Track complaint      → Ticket ID → in progress / resolved / invalid
        ├── 3  Schedule meeting     → name/date/reason → time slot → confirmation
        └── 4–13 Info replies       → localised text + closing note, menu re-shown
```

- All texts are localised in **English / Marathi / Hindi**.
- The menu is a WhatsApp list split into two sections (rows 1–10 and 11–13)
  because Meta allows max 10 rows per section.
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
| `PORT` / `ENV` | 8080 / development | HTTP server / JSON logs in production |
| `MIGRATIONS_DIR` | `migrations` | where `*.sql` migrations live |

## Database

**Pre-existing** (same as the original backend, created outside this repo):

- `get_nagarsevaks_by_pincode(TEXT)` – location + leaders for a PIN code
- `get_nagarsevak_by_id(TEXT)` – single leader
- `upsert_citizen_ivr(phone, lang, pincode, ward, nagarsevak_id)` – citizen persistence

**Created by this service** (applied automatically at startup,
`migrations/001_complaints_and_meetings.sql`, tracked in `schema_migrations`):

- `complaints` – `ticket_id` (AJ100001…), `phone`, `lang`, `full_name`,
  `description`, `address`, `leader_id`, `pincode`, `status`, timestamps
- `meetings` – `phone`, `lang`, `full_name`, `preferred_date`, `reason`,
  `slot`, `leader_id`, `pincode`, `created_at`
- `complaint_ticket_seq` – sequence generating the ticket IDs

### Marking a complaint resolved

```sql
UPDATE complaints SET status = 'resolved', updated_at = NOW()
WHERE ticket_id = 'AJ100001';
```

Anything other than `resolved` is reported as “in progress”.

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
so table creation works inside the container.
