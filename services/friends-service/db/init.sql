CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE IF NOT EXISTS friends (
    user_id1   UUID NOT NULL,
    user_id2   UUID NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now(),
    PRIMARY KEY (user_id1, user_id2)
);

CREATE TABLE IF NOT EXISTS friend_requests (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    sender_id   UUID NOT NULL,
    receiver_id UUID NOT NULL,
    status      VARCHAR(20) DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'declined')),
    created_at  TIMESTAMPTZ DEFAULT now(),
    CONSTRAINT uq_friend_request UNIQUE (sender_id, receiver_id)
);
