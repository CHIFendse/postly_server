CREATE TABLE IF NOT EXISTS users (
    user_id  UUID PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    email    TEXT NOT NULL UNIQUE,
    phone    TEXT NOT NULL UNIQUE
);

CREATE INDEX IF NOT EXISTS idx_users_username ON users (LOWER(username));

CREATE TABLE IF NOT EXISTS user_avatars (
    s3_key     TEXT PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_user_avatars_user_created ON user_avatars (user_id, created_at DESC);
