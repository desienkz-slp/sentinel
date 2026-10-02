-- NOC Sentinel — Case Engine foundation (additive migration)
-- Apply only via the migration runner introduced with PostgreSQL repository wiring.
-- This migration intentionally does not modify legacy incidents/audit JSON history.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS schema_migrations (
    version       TEXT PRIMARY KEY,
    applied_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    checksum      TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS conversations (
    conversation_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel         VARCHAR(64) NOT NULL,
    identity        VARCHAR(128) NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(channel, identity)
);

CREATE TABLE IF NOT EXISTS conversation_messages (
    message_pk      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL REFERENCES conversations(conversation_id),
    event_id        VARCHAR(255) NOT NULL,
    message_id      VARCHAR(255),
    direction       VARCHAR(16) NOT NULL CHECK (direction IN ('INBOUND', 'OUTBOUND')),
    body            TEXT NOT NULL,
    received_at     TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(event_id),
    UNIQUE(conversation_id, message_id)
);

CREATE TYPE case_state AS ENUM (
    'NEW', 'IDENTIFYING', 'CONVERSATION', 'INFORMATION_GATHERING',
    'WAITING_CUSTOMER', 'READY_FOR_DIAGNOSIS', 'REASONING', 'INVESTIGATION',
    'ACTION_PROPOSED', 'POLICY_CHECK', 'EXECUTING', 'VERIFYING', 'FAILED',
    'ESCALATION', 'HUMAN_HANDLING', 'RESOLVED'
);

CREATE TABLE IF NOT EXISTS cases (
    case_pk          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id          VARCHAR(32) NOT NULL UNIQUE,
    conversation_id  UUID REFERENCES conversations(conversation_id),
    customer_id      UUID,
    channel          VARCHAR(64) NOT NULL,
    identity         VARCHAR(128) NOT NULL,
    state            case_state NOT NULL DEFAULT 'NEW',
    domain           VARCHAR(64),
    intent           VARCHAR(128),
    severity         VARCHAR(16),
    collected_info   JSONB NOT NULL DEFAULT '{}'::jsonb,
    summary          JSONB NOT NULL DEFAULT '{}'::jsonb,
    version          BIGINT NOT NULL DEFAULT 1,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at      TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_cases_state_updated ON cases(state, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_cases_identity_state ON cases(identity, state);

CREATE TABLE IF NOT EXISTS case_events (
    event_pk       BIGSERIAL PRIMARY KEY,
    case_pk        UUID NOT NULL REFERENCES cases(case_pk),
    from_state     case_state,
    to_state       case_state NOT NULL,
    actor          VARCHAR(128) NOT NULL,
    reason         TEXT,
    trace_id       VARCHAR(128),
    details        JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_case_events_case_time ON case_events(case_pk, occurred_at);

CREATE TABLE IF NOT EXISTS case_verifications (
    verification_pk UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    case_pk         UUID NOT NULL REFERENCES cases(case_pk),
    passed          BOOLEAN NOT NULL,
    source          VARCHAR(128) NOT NULL,
    summary         TEXT NOT NULL,
    evidence        JSONB NOT NULL DEFAULT '{}'::jsonb,
    verified_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Database juga menegakkan invariant domain: update langsung tidak boleh
-- menandai RESOLVED tanpa bukti verification lulus yang telah tersimpan.
CREATE OR REPLACE FUNCTION enforce_case_resolution_verification()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.state = 'RESOLVED' AND NOT EXISTS (
        SELECT 1 FROM case_verifications
        WHERE case_pk = NEW.case_pk AND passed = TRUE
    ) THEN
        RAISE EXCEPTION 'case tidak boleh RESOLVED tanpa verification berhasil';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_cases_require_verification ON cases;
CREATE TRIGGER trg_cases_require_verification
BEFORE INSERT OR UPDATE OF state ON cases
FOR EACH ROW EXECUTE FUNCTION enforce_case_resolution_verification();

CREATE TABLE IF NOT EXISTS case_escalations (
    escalation_pk   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    case_pk         UUID NOT NULL REFERENCES cases(case_pk),
    domain          VARCHAR(64) NOT NULL,
    target_role     VARCHAR(64) NOT NULL,
    reason          TEXT NOT NULL,
    payload         JSONB NOT NULL,
    status          VARCHAR(32) NOT NULL DEFAULT 'REQUESTED',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    acknowledged_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS human_decisions (
    decision_pk     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    case_pk         UUID NOT NULL REFERENCES cases(case_pk),
    staff_id        VARCHAR(128) NOT NULL,
    decision        VARCHAR(64) NOT NULL,
    facts           JSONB NOT NULL DEFAULT '{}'::jsonb,
    note            TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS processed_events (
    event_id        VARCHAR(255) PRIMARY KEY,
    channel         VARCHAR(64) NOT NULL,
    case_pk         UUID REFERENCES cases(case_pk),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS case_leases (
    case_pk         UUID PRIMARY KEY REFERENCES cases(case_pk),
    holder          VARCHAR(128) NOT NULL,
    expires_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS outbox (
    outbox_pk       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    case_pk         UUID REFERENCES cases(case_pk),
    dedupe_key      VARCHAR(255) NOT NULL UNIQUE,
    kind            VARCHAR(64) NOT NULL,
    payload         JSONB NOT NULL,
    status          VARCHAR(32) NOT NULL DEFAULT 'PENDING',
    attempts        INTEGER NOT NULL DEFAULT 0,
    available_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at         TIMESTAMPTZ,
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_outbox_pending ON outbox(status, available_at);

-- Context pelanggan adalah cache/proyeksi fakta dari sistem sumber, bukan
-- pengganti status live Billing/RADIUS/perangkat.
CREATE TABLE IF NOT EXISTS customer_services (
    service_pk       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id      UUID NOT NULL REFERENCES customers(customer_id),
    external_id      VARCHAR(128),
    username         VARCHAR(255),
    package_name     VARCHAR(255),
    account_status   VARCHAR(64),
    router_name      VARCHAR(255),
    radius_name      VARCHAR(255),
    source           VARCHAR(64) NOT NULL,
    source_updated_at TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(customer_id, external_id)
);
CREATE INDEX IF NOT EXISTS idx_customer_services_customer ON customer_services(customer_id);

CREATE TABLE IF NOT EXISTS customer_context_snapshots (
    snapshot_pk      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id      UUID NOT NULL REFERENCES customers(customer_id),
    case_pk          UUID REFERENCES cases(case_pk),
    source           VARCHAR(64) NOT NULL,
    context          JSONB NOT NULL,
    observed_at      TIMESTAMPTZ NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_customer_context_customer_time ON customer_context_snapshots(customer_id, observed_at DESC);

-- Direktori staff menyimpan authority dan availability; nomor WhatsApp saja
-- tidak cukup untuk routing handoff yang aman.
CREATE TABLE IF NOT EXISTS staff (
    staff_id          VARCHAR(128) PRIMARY KEY,
    name              VARCHAR(255) NOT NULL,
    role              VARCHAR(64) NOT NULL CHECK (role IN ('noc_senior', 'admin')),
    department        VARCHAR(128),
    phone             VARCHAR(64),
    whatsapp_jid      VARCHAR(255),
    active            BOOLEAN NOT NULL DEFAULT TRUE,
    escalation_level  INTEGER NOT NULL DEFAULT 1 CHECK (escalation_level > 0),
    priority          INTEGER NOT NULL DEFAULT 100,
    working_hours     JSONB NOT NULL DEFAULT '{}'::jsonb,
    skills            JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_staff_role_active ON staff(role, active, priority);

CREATE TABLE IF NOT EXISTS staff_availability (
    availability_pk   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    staff_id          VARCHAR(128) NOT NULL REFERENCES staff(staff_id),
    on_call           BOOLEAN NOT NULL DEFAULT FALSE,
    starts_at         TIMESTAMPTZ,
    ends_at           TIMESTAMPTZ,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_staff_availability_on_call ON staff_availability(staff_id, on_call, ends_at);

CREATE TABLE IF NOT EXISTS staff_permissions (
    permission_pk     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    staff_id          VARCHAR(128) NOT NULL REFERENCES staff(staff_id),
    permission        VARCHAR(128) NOT NULL,
    granted_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at        TIMESTAMPTZ,
    UNIQUE(staff_id, permission)
);

-- Tambahan field audit Phase 1 tetap additive terhadap audit_logs dari 001.
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS case_pk UUID REFERENCES cases(case_pk);
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS trace_id VARCHAR(128);
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS agent VARCHAR(128);
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS model VARCHAR(128);
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS prompt_version VARCHAR(128);
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS tool VARCHAR(128);
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS arguments_sanitized JSONB;
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS result JSONB;
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS risk_level VARCHAR(32);
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS policy_decision VARCHAR(64);
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS execution_status VARCHAR(64);
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS verification VARCHAR(64);
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS human_approval VARCHAR(64);
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS error TEXT;
CREATE INDEX IF NOT EXISTS idx_audit_case_time ON audit_logs(case_pk, occurred_at DESC);
