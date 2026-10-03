CREATE TABLE IF NOT EXISTS credentials (
    user_id       UUID PRIMARY KEY,
    password_hash TEXT NOT NULL
);
