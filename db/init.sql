CREATE TABLE IF NOT EXISTS accounts (
    id         BIGINT      PRIMARY KEY,
    balance    BIGINT      NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS transactions (
    id           BIGSERIAL   PRIMARY KEY,
    account_id   BIGINT      NOT NULL REFERENCES accounts (id),
    amount_cents BIGINT      NOT NULL CHECK (amount_cents > 0),
    type         TEXT        NOT NULL CHECK (type IN ('credit', 'debit')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_transactions_account_id_id
    ON transactions (account_id, id DESC);
