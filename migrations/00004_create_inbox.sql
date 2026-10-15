-- +goose Up
CREATE TABLE inbox (
    id SERIAL PRIMARY KEY,
    consumer_name VARCHAR(255) NOT NULL,
    message_id VARCHAR(255) NOT NULL,
    payload_hash VARCHAR(255),
    status VARCHAR(50) NOT NULL DEFAULT 'PROCESSED',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (consumer_name, message_id)
);

-- +goose Down
DROP TABLE IF EXISTS inbox;
