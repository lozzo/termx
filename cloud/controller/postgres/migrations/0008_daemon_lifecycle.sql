ALTER TABLE daemons ADD COLUMN state text;

UPDATE daemons
SET state = CASE WHEN revoked THEN 'blocked' ELSE 'active' END;

ALTER TABLE daemons
    ALTER COLUMN state SET NOT NULL,
    ADD CONSTRAINT daemons_state_check CHECK (state IN ('active', 'blocked', 'deleted')),
    DROP CONSTRAINT daemons_device_id_key,
    DROP CONSTRAINT daemons_device_fingerprint_key,
    DROP COLUMN revoked;

ALTER TABLE daemons RENAME COLUMN revision TO state_revision;

CREATE UNIQUE INDEX daemons_current_device_id_idx
    ON daemons(device_id) WHERE state <> 'deleted';

CREATE UNIQUE INDEX daemons_current_device_fingerprint_idx
    ON daemons(device_fingerprint) WHERE state <> 'deleted';
