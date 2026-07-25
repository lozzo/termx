CREATE TABLE release_artifacts (
    release_id TEXT PRIMARY KEY,
    product INTEGER NOT NULL,
    channel INTEGER NOT NULL,
    target_os TEXT NOT NULL,
    target_arch TEXT NOT NULL,
    version_code BIGINT NOT NULL,
    published_at BIGINT NOT NULL,
    projection BYTEA NOT NULL,
    UNIQUE(product, channel, target_os, target_arch, version_code)
);

CREATE TABLE release_channel_heads (
    product INTEGER NOT NULL,
    channel INTEGER NOT NULL,
    target_os TEXT NOT NULL,
    target_arch TEXT NOT NULL,
    active_release_id TEXT NOT NULL REFERENCES release_artifacts(release_id),
    revision BIGINT NOT NULL,
    paused BOOLEAN NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY(product, channel, target_os, target_arch)
);
