-- Operator-managed model gateways, independent of coding-agent CLIs.
-- Credentials are encrypted with the instance secrets key before persistence.
CREATE TABLE model_provider (
    id TEXT PRIMARY KEY,
    revision TEXT NOT NULL,
    config_json TEXT NOT NULL,
    key_ciphertext BLOB,
    key_nonce BLOB
);
