CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE IF NOT EXISTS messages (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    chat_id    UUID NOT NULL,
    sender_id  UUID NOT NULL,
    text       TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now(),
    type       TEXT DEFAULT 'text',
    file_url   TEXT,
    file_size  INTEGER,
    file_name  TEXT,
    CONSTRAINT chk_messages_type CHECK (type IN ('text', 'image', 'video', 'voice', 'file'))
);

CREATE INDEX IF NOT EXISTS idx_messages_conv ON messages (chat_id);
CREATE INDEX IF NOT EXISTS idx_messages_time ON messages (created_at);
