ALTER TABLE edge_claim_tokens
    DROP CONSTRAINT edge_claim_tokens_purpose_check;

ALTER TABLE edge_claim_tokens
    ADD CONSTRAINT edge_claim_tokens_purpose_check
    CHECK (purpose IN ('install', 'bootstrap', 'identity_recovery'));
