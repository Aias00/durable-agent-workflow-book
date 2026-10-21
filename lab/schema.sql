CREATE TABLE workflows (
 id text PRIMARY KEY,
 tenant text NOT NULL,
 operation text NOT NULL,
 plan jsonb NOT NULL,
 fingerprint text NOT NULL,
 plan_version integer NOT NULL,
 state text NOT NULL CHECK (state IN ('waiting_approval','ready','running','unknown','succeeded','cancelled','failed','incompatible')),
 approved_by text NOT NULL DEFAULT '',
 approval_expires timestamptz,
 owner text NOT NULL DEFAULT '',
 fence bigint NOT NULL DEFAULT 0,
 attempt integer NOT NULL DEFAULT 0 CHECK (attempt>=0),
 max_attempts integer NOT NULL DEFAULT 3 CHECK (max_attempts BETWEEN 1 AND 10),
 lease_until timestamptz,
 ready_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 cancel_requested boolean NOT NULL DEFAULT false,
 result jsonb,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE (tenant, operation)
);
CREATE INDEX ready_work ON workflows (ready_at, created_at) WHERE state='ready';
CREATE TABLE events (
 seq bigserial PRIMARY KEY,
 workflow_id text NOT NULL REFERENCES workflows(id),
 kind text NOT NULL,
 fence bigint NOT NULL,
 detail text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE tool_effects (
 seq bigserial PRIMARY KEY,
 effect_key text NOT NULL UNIQUE,
 fingerprint text NOT NULL,
 account text NOT NULL,
 amount_minor bigint NOT NULL CHECK (amount_minor>0),
 receipt text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
