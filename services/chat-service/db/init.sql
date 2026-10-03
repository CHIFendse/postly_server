CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE IF NOT EXISTS conversations (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    type       VARCHAR(20) NOT NULL CHECK (type IN ('private', 'group')),
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS chats (
    id              UUID PRIMARY KEY,
    user_id1        UUID NOT NULL,
    user_id2        UUID NOT NULL,
    last_message    TEXT,
    last_msg_sender UUID,
    updated_at      TIMESTAMPTZ DEFAULT now(),
    created_at      TIMESTAMPTZ DEFAULT now(),
    CONSTRAINT unique_chat_pair UNIQUE (user_id1, user_id2),
    CONSTRAINT different_users  CHECK (user_id1 <> user_id2)
);

CREATE TABLE IF NOT EXISTS groups (
    id              UUID PRIMARY KEY,
    name            VARCHAR(100) NOT NULL,
    admin_id        UUID NOT NULL,
    is_private      BOOLEAN DEFAULT false,
    last_message    TEXT,
    last_msg_sender UUID,
    created_at      TIMESTAMPTZ DEFAULT now(),
    updated_at      TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS group_members (
    group_id  UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id   UUID NOT NULL,
    role      VARCHAR(20) DEFAULT 'member' CHECK (role IN ('admin', 'member')),
    joined_at TIMESTAMPTZ DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_chats_user_id1     ON chats (user_id1);
CREATE INDEX IF NOT EXISTS idx_chats_user_id2     ON chats (user_id2);
CREATE INDEX IF NOT EXISTS idx_chats_updated_at   ON chats (updated_at);
CREATE INDEX IF NOT EXISTS idx_groups_updated_at  ON groups (updated_at);
CREATE INDEX IF NOT EXISTS idx_gm_user_id         ON group_members (user_id);

CREATE OR REPLACE FUNCTION update_updated_at() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END; $$;

CREATE OR REPLACE FUNCTION sync_conversation_chat() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO conversations (id, type) VALUES (NEW.id, 'private') ON CONFLICT DO NOTHING;
    RETURN NEW;
END; $$;

CREATE OR REPLACE FUNCTION sync_conversation_group() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO conversations (id, type) VALUES (NEW.id, 'group') ON CONFLICT DO NOTHING;
    RETURN NEW;
END; $$;

CREATE OR REPLACE TRIGGER chats_updated_at
    BEFORE UPDATE ON chats FOR EACH ROW EXECUTE FUNCTION update_updated_at();
CREATE OR REPLACE TRIGGER groups_updated_at
    BEFORE UPDATE ON groups FOR EACH ROW EXECUTE FUNCTION update_updated_at();
CREATE OR REPLACE TRIGGER conv_on_chat
    BEFORE INSERT ON chats FOR EACH ROW EXECUTE FUNCTION sync_conversation_chat();
CREATE OR REPLACE TRIGGER conv_on_group
    BEFORE INSERT ON groups FOR EACH ROW EXECUTE FUNCTION sync_conversation_group();
