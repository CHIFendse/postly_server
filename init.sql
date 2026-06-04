-- Postly database schema
-- Runs once on first postgres container startup (docker-compose mount).

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
SET timezone = 'UTC';

-- ── user-service ─────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS users (
    user_id  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    username VARCHAR(50)  UNIQUE NOT NULL,
    email    VARCHAR(255) UNIQUE NOT NULL,
    phone    VARCHAR(20)
);

-- ── auth-service ─────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS credentials (
    user_id       UUID PRIMARY KEY REFERENCES users(user_id) ON DELETE CASCADE,
    password_hash TEXT NOT NULL
);

-- ── chat-service: private chats ──────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS chats (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id1        UUID NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    user_id2        UUID NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    last_message    TEXT,
    last_msg_sender UUID REFERENCES users(user_id) ON DELETE SET NULL,
    updated_at      TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    created_at      TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chats_unique_pair   UNIQUE (user_id1, user_id2),
    CONSTRAINT chats_different_users CHECK (user_id1 <> user_id2)
);

-- ── chat-service: groups ─────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS groups (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name            VARCHAR(100) NOT NULL,
    admin_id        UUID NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    is_private      BOOLEAN DEFAULT FALSE,
    last_message    TEXT,
    last_msg_sender UUID REFERENCES users(user_id) ON DELETE SET NULL,
    created_at      TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS group_members (
    group_id  UUID REFERENCES groups(id) ON DELETE CASCADE,
    user_id   UUID REFERENCES users(user_id) ON DELETE CASCADE,
    role      VARCHAR(20) DEFAULT 'member' CHECK (role IN ('admin', 'member')),
    joined_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (group_id, user_id)
);

-- ── messaging-service ────────────────────────────────────────────────────────
-- conversation_id = chat.id or group.id (same UUID namespace)
CREATE TABLE IF NOT EXISTS messages (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    conversation_id UUID NOT NULL,  -- FK not enforced so chats and groups share the table
    sender_id       UUID REFERENCES users(user_id) ON DELETE SET NULL,
    text            TEXT NOT NULL,
    created_at      TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- ── friends-service ──────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS friends (
    user_id1   UUID REFERENCES users(user_id) ON DELETE CASCADE,
    user_id2   UUID REFERENCES users(user_id) ON DELETE CASCADE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id1, user_id2),
    CONSTRAINT friends_ordered CHECK (user_id1 < user_id2)
);

CREATE TABLE IF NOT EXISTS friend_requests (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    sender_id   UUID NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    receiver_id UUID NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
    status      VARCHAR(20) DEFAULT 'pending' CHECK (status IN ('pending','accepted','declined')),
    created_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT friend_requests_unique UNIQUE (sender_id, receiver_id)
);

-- ── Indexes ──────────────────────────────────────────────────────────────────
CREATE INDEX IF NOT EXISTS idx_chats_user1      ON chats(user_id1);
CREATE INDEX IF NOT EXISTS idx_chats_user2      ON chats(user_id2);
CREATE INDEX IF NOT EXISTS idx_chats_updated    ON chats(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_groups_updated   ON groups(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_group_members_u  ON group_members(user_id);
CREATE INDEX IF NOT EXISTS idx_messages_conv    ON messages(conversation_id);
CREATE INDEX IF NOT EXISTS idx_messages_created ON messages(created_at);
CREATE INDEX IF NOT EXISTS idx_freq_receiver    ON friend_requests(receiver_id) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_freq_sender      ON friend_requests(sender_id)   WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_users_username   ON users(LOWER(username));

-- ── Triggers: auto-update updated_at ────────────────────────────────────────
CREATE OR REPLACE FUNCTION update_updated_at()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'trg_chats_updated_at') THEN
        CREATE TRIGGER trg_chats_updated_at
            BEFORE UPDATE ON chats
            FOR EACH ROW EXECUTE FUNCTION update_updated_at();
    END IF;
END $$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'trg_groups_updated_at') THEN
        CREATE TRIGGER trg_groups_updated_at
            BEFORE UPDATE ON groups
            FOR EACH ROW EXECUTE FUNCTION update_updated_at();
    END IF;
END $$;
