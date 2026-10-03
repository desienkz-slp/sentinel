-- NOC Sentinel — Autonomous NOC Database Schema
-- Version: 1.0.0
-- Compatible: PostgreSQL 14+ (pgvector opsional untuk kolom embedding)

-- ============================================================================
-- ENUMS & BASE TABLES
-- ============================================================================

CREATE TYPE incident_status AS ENUM ('OPEN', 'INVESTIGATING', 'RESOLVED', 
                                      'ESCALATED', 'WAITING', 'CLOSED', 'UNKNOWN');

CREATE TYPE tool_permission AS ENUM ('READ', 'WRITE', 'ADMIN');
CREATE TYPE action_risk AS ENUM ('LOW', 'MEDIUM', 'HIGH');
CREATE TYPE decision_result AS ENUM ('ALLOW', 'APPROVAL_REQUIRED', 'DENY');

-- customers: master identity mapping
CREATE TABLE customers (
    customer_id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    phone_normalized    VARCHAR(32) NOT NULL UNIQUE,
    aliases             TEXT[] DEFAULT '{}',        -- alternative phone formats
    name                VARCHAR(255),
    organization        VARCHAR(255),
    notes               JSONB DEFAULT '{}',
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    updated_at          TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX idx_customers_phone ON customers(phone_normalized);

-- devices: CPE, ONT, router links
CREATE TABLE devices (
    device_id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id         UUID NOT NULL REFERENCES customers(customer_id),
    device_type         VARCHAR(64) NOT NULL,       -- ONU, ROUTER, GATEWAY
    serial_number       VARCHAR(64),
    mac_address         VARCHAR(64),
    ip_address        VARCHAR(64),
    hostname            VARCHAR(255),
    firmware_version    VARCHAR(64),
    status              VARCHAR(64),                -- ONLINE/OFFLINE/etc
    last_seen_at        TIMESTAMPTZ,
    notes               JSONB DEFAULT '{}',
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    updated_at          TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(device_type, serial_number),
    UNIQUE(device_type, mac_address)
);
CREATE INDEX idx_devices_customer ON devices(customer_id);

-- incidents: core event table
CREATE TABLE incidents (
    incident_id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    status              incident_status NOT NULL DEFAULT 'OPEN',
    severity            VARCHAR(64),                -- normal/high/critical
    source              VARCHAR(64),                -- whatsapp, monitoring, manual
    intent_raw          TEXT,                       -- original customer message
    intent_normalized   VARCHAR(128),               -- e.g., CUSTOMER_INTERNET_DOWN
    confidence          NUMERIC,                    -- model confidence 0..1
    identity            VARCHAR(64) NOT NULL,       -- normalized sender
    affected_customer   UUID REFERENCES customers(customer_id),
    affected_device     UUID REFERENCES devices(device_id),
    environment         VARCHAR(64),                -- production/staging/dev
    started_at          TIMESTAMPTZ DEFAULT NOW(),
    closed_at           TIMESTAMPTZ,
    resolution_notes    TEXT,
    closure_reason      VARCHAR(64),                -- RESOLVED/ESCALATED/WAITING/CLOSED
    escalation_to       VARCHAR(255),               -- person/channel for escalations
    notes               JSONB DEFAULT '{}'
);
CREATE INDEX idx_incidents_status ON incidents(status);
CREATE INDEX idx_incidents_identity ON incidents(identity);
CREATE INDEX idx_incidents_customer ON incidents(affected_customer);
CREATE INDEX idx_incidents_started ON incidents(started_at);

-- incident_events: timeline of everything relevant
CREATE TABLE incident_events (
    event_id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    incident_id         UUID NOT NULL REFERENCES incidents(incident_id),
    event_type          VARCHAR(64) NOT NULL,       -- EVIDENCE_GATHERED, TOOL_CALLED, DIAGNOSIS, ACTION_STARTED, ACTION_SUCCESS, VERIFIED, ESCALATION_REQUESTED
    timestamp           TIMESTAMPTZ DEFAULT NOW(),
    trace_id            VARCHAR(128),
    actor               VARCHAR(128),               -- SYSTEM, OPERATOR_XYZ
    details             JSONB DEFAULT '{}'
);
CREATE INDEX idx_events_incident ON incident_events(incident_id);
CREATE INDEX idx_events_timestamp ON incident_events(timestamp);

-- diagnoses: reasoning outputs
CREATE TABLE diagnoses (
    diagnosis_id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    incident_id         UUID NOT NULL REFERENCES incidents(incident_id),
    root_cause          VARCHAR(255),
    confidence          NUMERIC,
    evidence_summary    TEXT,
    missing_evidence    TEXT[],
    alternatives        TEXT[],
    proposed_action     TEXT,
    risk_level          action_risk,
    policy_decision     decision_result,
    approved_by         VARCHAR(255),
    approved_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(incident_id)
);

-- tool_calls: exact invocation record
CREATE TABLE tool_calls (
    call_id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    incident_id         UUID NOT NULL REFERENCES incidents(incident_id),
    tool_name           VARCHAR(128) NOT NULL,
    version             VARCHAR(64),
    request_id          VARCHAR(128) NOT NULL,
    scope               VARCHAR(128),
    permission          tool_permission,
    params              JSONB,
    response            JSONB,
    latency_ms          INTEGER,
    succeeded           BOOLEAN,
    error_code          VARCHAR(64),
    error_message       TEXT,
    idempotency_key     VARCHAR(256),
    dry_run             BOOLEAN DEFAULT FALSE,
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(request_id)
);
CREATE INDEX idx_tool_calls_incident ON tool_calls(incident_id);
CREATE INDEX idx_tool_calls_request ON tool_calls(request_id);

-- workflows & workflow_runs
CREATE TABLE workflows (
    workflow_id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                VARCHAR(128) NOT NULL,
    description         TEXT,
    version             VARCHAR(32) NOT NULL,
    trigger             VARCHAR(128),               -- intent match pattern
    steps_spec          JSONB,                      -- deterministic step spec
    active              BOOLEAN DEFAULT TRUE,
    UNIQUE(name, version)
);

CREATE TABLE workflow_runs (
    run_id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id         UUID NOT NULL REFERENCES workflows(workflow_id),
    incident_id         UUID REFERENCES incidents(incident_id),
    started_at          TIMESTAMPTZ DEFAULT NOW(),
    completed_at        TIMESTAMPTZ,
    status              VARCHAR(64),                -- RUNNING/SUCCESS/FAILED/TIMEOUT
    result              JSONB,
    UNIQUE(run_id, workflow_id, incident_id)
);

CREATE TABLE workflow_steps (
    step_id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id              UUID NOT NULL REFERENCES workflow_runs(run_id),
    step_index          INTEGER NOT NULL,
    name                VARCHAR(128) NOT NULL,
    status              VARCHAR(64),                -- PENDING/RUNNING/SKIPPED/SUCCESS/FAILED
    started_at          TIMESTAMPTZ,
    completed_at        TIMESTAMPTZ,
    result              JSONB,
    UNIQUE(run_id, step_index)
);

-- approvals: audit trail for human authorizations
CREATE TABLE approvals (
    approval_id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    incident_id         UUID NOT NULL REFERENCES incidents(incident_id),
    action_description  TEXT NOT NULL,
    tool_name           VARCHAR(128),
    scope               VARCHAR(128),
    risk_level          action_risk,
    requested_by        VARCHAR(128),
    approved_by         VARCHAR(128),
    decision            decision_result,
    reason              TEXT,
    metadata            JSONB DEFAULT '{}',
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    reviewed_at         TIMESTAMPTZ,
    expires_at          TIMESTAMPTZ
);
CREATE INDEX idx_approvals_expires ON approvals(expires_at);

-- audit_logs: immutable action log
CREATE TABLE audit_logs (
    log_id              BIGSERIAL PRIMARY KEY,
    event_type          VARCHAR(128) NOT NULL,
    occurred_at         TIMESTAMPTZ DEFAULT NOW(),
    actor               VARCHAR(128) NOT NULL,
    entity_type         VARCHAR(64),                -- customer/device/incident/policy/tool/workflow
    entity_id           UUID,
    before              JSONB,
    after               JSONB,
    request_id          VARCHAR(128),
    note                TEXT
);
CREATE INDEX idx_audit_acted_at ON audit_logs(occurred_at DESC);

-- skills & runbooks (structured knowledge)
CREATE TABLE skills (
    skill_id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                VARCHAR(128) NOT NULL,
    description         TEXT,
    version             VARCHAR(32) NOT NULL,
    trigger_intent      VARCHAR(128),
    required_tools      TEXT[],
    risk_level          action_risk,
    auto_execute        BOOLEAN DEFAULT FALSE,
    approval_required   BOOLEAN DEFAULT TRUE,
    content             TEXT,                       -- skill.md body
    verified            BOOLEAN DEFAULT FALSE,
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    verified_at         TIMESTAMPTZ,
    UNIQUE(name, version)
);

-- knowledge_documents (for vector search)
CREATE TABLE knowledge_documents (
    doc_id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title               VARCHAR(255),
    category            VARCHAR(128),
    content             TEXT NOT NULL,
    source_ref          VARCHAR(255),
    verified            BOOLEAN DEFAULT FALSE,
    created_at          TIMESTAMPTZ DEFAULT NOW()
);

-- Kolom embedding HANYA dibuat bila pgvector tersedia (PostgreSQL biasa tetap
-- bisa menjalankan migrasi ini; pencarian vektor adalah fitur opsional).
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'vector') THEN
        CREATE EXTENSION IF NOT EXISTS vector;
        ALTER TABLE knowledge_documents ADD COLUMN IF NOT EXISTS embedding vector(1536);
    END IF;
END
$$;
