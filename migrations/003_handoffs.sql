-- NOC Sentinel - buku serah-terima CS <-> NOC (Fase D).
-- Aditif: tidak mengubah tabel yang sudah ada.

CREATE TABLE IF NOT EXISTS handoffs (
    case_id     VARCHAR(64) PRIMARY KEY,
    customer    VARCHAR(128) NOT NULL,
    domain      VARCHAR(64)  NOT NULL,
    complaint   TEXT NOT NULL DEFAULT '',
    evidence    JSONB NOT NULL DEFAULT '{}'::jsonb,
    severity    VARCHAR(16),
    status      VARCHAR(32) NOT NULL DEFAULT 'open',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_handoffs_status ON handoffs(status, updated_at DESC);

CREATE TABLE IF NOT EXISTS handoff_updates (
    update_pk       BIGSERIAL PRIMARY KEY,
    case_id         VARCHAR(64) NOT NULL REFERENCES handoffs(case_id) ON DELETE CASCADE,
    seq             INTEGER NOT NULL,
    at              TIMESTAMPTZ NOT NULL,
    by_role         VARCHAR(128) NOT NULL,
    status          VARCHAR(32) NOT NULL,
    untuk_pelanggan TEXT NOT NULL DEFAULT '',
    notified        BOOLEAN NOT NULL DEFAULT FALSE,
    notify_err      TEXT NOT NULL DEFAULT '',
    UNIQUE (case_id, seq)
);
