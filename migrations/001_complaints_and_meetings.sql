-- New tables for the citizen chatbot complaint / meeting flows.
-- Idempotent: safe to run more than once.

CREATE TABLE IF NOT EXISTS complaints (
    ticket_id   TEXT PRIMARY KEY,
    phone       TEXT NOT NULL,
    lang        TEXT NOT NULL DEFAULT 'en',
    full_name   TEXT NOT NULL,
    description TEXT NOT NULL,
    address     TEXT NOT NULL,
    leader_id   TEXT,
    pincode     TEXT,
    status      TEXT NOT NULL DEFAULT 'in_progress',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_complaints_phone ON complaints(phone);

-- Sequential ticket IDs: AJ100001, AJ100002, ...
CREATE SEQUENCE IF NOT EXISTS complaint_ticket_seq START 100001;

CREATE TABLE IF NOT EXISTS meetings (
    id             BIGSERIAL PRIMARY KEY,
    phone          TEXT NOT NULL,
    lang           TEXT NOT NULL DEFAULT 'en',
    full_name      TEXT NOT NULL,
    preferred_date DATE NOT NULL,
    reason         TEXT NOT NULL,
    slot           TEXT NOT NULL,
    leader_id      TEXT,
    pincode        TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_meetings_phone ON meetings(phone);
