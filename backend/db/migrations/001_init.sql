-- Enable TimescaleDB extension
CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE;

-- Users table for JWT auth
CREATE TABLE IF NOT EXISTS users (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email       TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role        TEXT NOT NULL CHECK (role IN ('admin', 'producer', 'responder')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Work items (incidents)
CREATE TABLE IF NOT EXISTS work_items (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    component_id    TEXT NOT NULL,
    component_type  TEXT NOT NULL,
    severity        TEXT NOT NULL CHECK (severity IN ('P0', 'P1', 'P2', 'P3')),
    status          TEXT NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN', 'INVESTIGATING', 'RESOLVED', 'CLOSED')),
    title           TEXT NOT NULL,
    signal_count    INTEGER NOT NULL DEFAULT 1,
    first_signal_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_signal_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_work_items_status ON work_items(status);
CREATE INDEX IF NOT EXISTS idx_work_items_severity ON work_items(severity);
CREATE INDEX IF NOT EXISTS idx_work_items_component ON work_items(component_id);

-- State transitions audit log
CREATE TABLE IF NOT EXISTS state_transitions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    work_item_id    UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
    from_state      TEXT,
    to_state        TEXT NOT NULL,
    transitioned_by UUID REFERENCES users(id),
    transitioned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    notes           TEXT
);

CREATE INDEX IF NOT EXISTS idx_transitions_work_item ON state_transitions(work_item_id);

-- Alert records
CREATE TABLE IF NOT EXISTS alerts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    work_item_id    UUID NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
    priority        TEXT NOT NULL,
    channel         TEXT NOT NULL,
    payload         JSONB,
    sent_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- RCA (Root Cause Analysis)
CREATE TABLE IF NOT EXISTS rca (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    work_item_id        UUID NOT NULL UNIQUE REFERENCES work_items(id) ON DELETE CASCADE,
    category            TEXT NOT NULL CHECK (category <> ''),
    fix_applied         TEXT NOT NULL CHECK (fix_applied <> ''),
    prevention_steps    TEXT NOT NULL CHECK (prevention_steps <> ''),
    incident_start      TIMESTAMPTZ NOT NULL,
    incident_end        TIMESTAMPTZ NOT NULL,
    submitted_by        UUID REFERENCES users(id),
    submitted_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT end_after_start CHECK (incident_end > incident_start)
);

-- Timeseries: signal counts per minute per component (hypertable)
CREATE TABLE IF NOT EXISTS signal_counts (
    bucket          TIMESTAMPTZ NOT NULL,
    component_id    TEXT NOT NULL,
    component_type  TEXT NOT NULL,
    severity        TEXT NOT NULL,
    count           INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket, component_id, severity)
);

SELECT create_hypertable('signal_counts', 'bucket', if_not_exists => TRUE);
CREATE INDEX IF NOT EXISTS idx_signal_counts_component ON signal_counts(component_id, bucket DESC);

-- Seed users (admin / producer / responder) — passwords are bcrypt of 'password123'
INSERT INTO users (id, email, password_hash, role) VALUES
    ('00000000-0000-0000-0000-000000000001', 'admin@ims.local',     '$2a$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/LewdBPj/o.k8VGwkm', 'admin'),
    ('00000000-0000-0000-0000-000000000002', 'producer@ims.local',  '$2a$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/LewdBPj/o.k8VGwkm', 'producer'),
    ('00000000-0000-0000-0000-000000000003', 'responder@ims.local', '$2a$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/LewdBPj/o.k8VGwkm', 'responder')
ON CONFLICT (email) DO NOTHING;
