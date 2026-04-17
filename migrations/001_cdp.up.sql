-- CDP 核心数据模型

CREATE TABLE IF NOT EXISTS cdp_profiles (
    id BIGSERIAL PRIMARY KEY,
    canonical_id VARCHAR(64) NOT NULL UNIQUE,
    external_ids JSONB NOT NULL DEFAULT '{}',
    attributes  JSONB NOT NULL DEFAULT '{}',
    traits      JSONB NOT NULL DEFAULT '{}',
    tags        TEXT[],
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_profile_external_ids ON cdp_profiles USING GIN (external_ids);
CREATE INDEX IF NOT EXISTS idx_profile_tags ON cdp_profiles USING GIN (tags);

CREATE TABLE IF NOT EXISTS cdp_events (
    id          BIGSERIAL,
    canonical_id VARCHAR(64) NOT NULL,
    event_type  VARCHAR(64)  NOT NULL,
    properties  JSONB,
    source      VARCHAR(30),
    occurred_at TIMESTAMPTZ  NOT NULL,
    received_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, occurred_at)
) PARTITION BY RANGE (occurred_at);

CREATE TABLE IF NOT EXISTS cdp_events_default PARTITION OF cdp_events DEFAULT;

CREATE INDEX IF NOT EXISTS idx_cdp_events_profile ON cdp_events(canonical_id, occurred_at DESC);

CREATE TABLE IF NOT EXISTS cdp_audiences (
    id              BIGSERIAL PRIMARY KEY,
    name            VARCHAR(255) NOT NULL UNIQUE,
    description     TEXT,
    conditions      JSONB        NOT NULL,
    type            VARCHAR(20)  NOT NULL DEFAULT 'dynamic',
    size_estimate   INT,
    last_computed_at TIMESTAMPTZ,
    status          VARCHAR(20)  NOT NULL DEFAULT 'active',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS cdp_audience_members (
    audience_id  BIGINT      NOT NULL REFERENCES cdp_audiences(id) ON DELETE CASCADE,
    canonical_id VARCHAR(64) NOT NULL,
    added_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY  (audience_id, canonical_id)
);
CREATE INDEX IF NOT EXISTS idx_audience_members_cid ON cdp_audience_members(canonical_id);

CREATE TABLE IF NOT EXISTS cdp_journeys (
    id              BIGSERIAL PRIMARY KEY,
    name            VARCHAR(255) NOT NULL UNIQUE,
    entry_condition JSONB,
    steps           JSONB        NOT NULL DEFAULT '[]',
    status          VARCHAR(20)  NOT NULL DEFAULT 'draft',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS cdp_journey_instances (
    id           BIGSERIAL PRIMARY KEY,
    journey_id   BIGINT      NOT NULL REFERENCES cdp_journeys(id) ON DELETE CASCADE,
    canonical_id VARCHAR(64) NOT NULL,
    current_step VARCHAR(64),
    state        JSONB        NOT NULL DEFAULT '{}',
    started_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    UNIQUE (journey_id, canonical_id)
);
CREATE INDEX IF NOT EXISTS idx_journey_instances_cid ON cdp_journey_instances(canonical_id);
