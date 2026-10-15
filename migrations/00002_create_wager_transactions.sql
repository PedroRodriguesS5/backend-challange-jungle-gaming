-- +goose Up
CREATE TABLE wager_transactions (
    id VARCHAR(64) PRIMARY KEY,
    external_id VARCHAR(255) NOT NULL,
    provider_id VARCHAR(255) NOT NULL,
    idempotency_key VARCHAR(255) NOT NULL,
    payload_hash VARCHAR(255) NOT NULL,
    wallet_id VARCHAR(64) NOT NULL,
    player_id VARCHAR(64) NOT NULL,
    round_id VARCHAR(255),
    game_id VARCHAR(255),
    kind VARCHAR(50) NOT NULL,
    amount NUMERIC(15,2) NOT NULL,
    currency VARCHAR(3) NOT NULL,
    reference_external_id VARCHAR(255),
    status VARCHAR(50) NOT NULL,
    failure_code VARCHAR(50),
    balance_after NUMERIC(15,2),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (provider_id, external_id),
    UNIQUE (provider_id, idempotency_key)
);

CREATE INDEX idx_wager_tx_wallet_id ON wager_transactions (wallet_id);
CREATE INDEX idx_wager_tx_status ON wager_transactions (status) WHERE status = 'PENDING_REFERENCE';

-- +goose Down
DROP TABLE IF EXISTS wager_transactions;
