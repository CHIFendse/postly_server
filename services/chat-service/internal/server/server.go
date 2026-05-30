package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	chatpb "postly/proto/chat"
)

const (
	chatsKeyPrefix        = "chat:chats:"
	groupsKeyPrefix       = "chat:groups:"
	participantsKeyPrefix = "chat:participants:"
	chatsCacheTTL         = 60 * time.Second
	participantsCacheTTL  = 5 * time.Minute
)

type Chat struct {
	chatpb.UnimplementedChatServiceServer
	db    *sql.DB
	cache *redis.Client
}

func New(db *sql.DB, cache *redis.Client) *Chat {
	return &Chat{db: db, cache: cache}
}

// ─── Chats ────────────────────────────────────────────────────────────────────

func (c *Chat) GetChats(ctx context.Context, req *chatpb.GetChatsRequest) (*chatpb.GetChatsResponse, error) {
	key := chatsKeyPrefix + req.UserId
	if cached, err := c.cache.Get(ctx, key).Bytes(); err == nil {
		var resp chatpb.GetChatsResponse
		if json.Unmarshal(cached, &resp) == nil {
			return &resp, nil
		}
	}

	// users нет в этой БД — возвращаем user_id2 как name, gateway резолвит username
	rows, err := c.db.QueryContext(ctx, `
		SELECT c.id,
		       CASE WHEN c.user_id1 = $1::uuid THEN c.user_id2::text ELSE c.user_id1::text END,
		       COALESCE(c.last_message, ''),
		       COALESCE(c.last_msg_sender::text, ''),
		       EXTRACT(EPOCH FROM c.updated_at)::INT
		FROM chats c
		WHERE c.user_id1 = $1::uuid OR c.user_id2 = $1::uuid
		ORDER BY c.updated_at DESC`, req.UserId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer rows.Close()

	resp := &chatpb.GetChatsResponse{}
	for rows.Next() {
		ch := &chatpb.Chat{}
		if err := rows.Scan(&ch.Id, &ch.Name, &ch.LastMessage, &ch.LastMsgSender, &ch.UpdatedAt); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		resp.Chats = append(resp.Chats, ch)
	}

	if data, err := json.Marshal(resp); err == nil {
		c.cache.Set(ctx, key, data, chatsCacheTTL)
	}
	return resp, nil
}

func (c *Chat) CreateChat(ctx context.Context, req *chatpb.CreateChatRequest) (*chatpb.CreateChatResponse, error) {
	// target_user_id приходит от gateway (тот спросил user-service по username)
	u1, u2 := req.UserId, req.TargetUsername // TargetUsername здесь уже UUID
	if u1 > u2 {
		u1, u2 = u2, u1
	}

	var existingID string
	err := c.db.QueryRowContext(ctx,
		"SELECT id FROM chats WHERE user_id1 = $1 AND user_id2 = $2", u1, u2,
	).Scan(&existingID)
	if err == nil {
		return &chatpb.CreateChatResponse{ChatId: existingID, TargetId: req.TargetUsername}, nil
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer tx.Rollback()

	var newID string
	if err = tx.QueryRowContext(ctx,
		"INSERT INTO conversations (type) VALUES ('private') RETURNING id",
	).Scan(&newID); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if _, err = tx.ExecContext(ctx,
		"INSERT INTO chats (id, user_id1, user_id2, updated_at) VALUES ($1, $2, $3, NOW())",
		newID, u1, u2,
	); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if err = tx.Commit(); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	c.cache.Del(ctx, chatsKeyPrefix+req.UserId, chatsKeyPrefix+req.TargetUsername)
	return &chatpb.CreateChatResponse{ChatId: newID, TargetId: req.TargetUsername}, nil
}

func (c *Chat) DeleteChat(ctx context.Context, req *chatpb.DeleteChatRequest) (*chatpb.DeleteChatResponse, error) {
	res, err := c.db.ExecContext(ctx,
		"DELETE FROM chats WHERE id=$1 AND (user_id1=$2 OR user_id2=$2)",
		req.ChatId, req.UserId,
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if n, _ := res.RowsAffected(); n > 0 {
		c.db.ExecContext(ctx, "DELETE FROM conversations WHERE id=$1", req.ChatId)
		c.invalidateChat(ctx, req.ChatId, req.UserId)
		return &chatpb.DeleteChatResponse{Ok: true}, nil
	}

	// Группа — только admin
	res, err = c.db.ExecContext(ctx,
		"DELETE FROM groups WHERE id=$1 AND admin_id=$2",
		req.ChatId, req.UserId,
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, status.Error(codes.PermissionDenied, "нет прав на удаление")
	}

	c.db.ExecContext(ctx, "DELETE FROM conversations WHERE id=$1", req.ChatId)
	c.invalidateChat(ctx, req.ChatId, req.UserId)
	return &chatpb.DeleteChatResponse{Ok: true}, nil
}

func (c *Chat) ClearChat(ctx context.Context, req *chatpb.ClearChatRequest) (*chatpb.ClearChatResponse, error) {
	var count int
	c.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM chats WHERE id=$1 AND (user_id1=$2 OR user_id2=$2)`,
		req.ChatId, req.UserId,
	).Scan(&count)
	if count == 0 {
		c.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM group_members WHERE group_id=$1 AND user_id=$2`,
			req.ChatId, req.UserId,
		).Scan(&count)
	}
	if count == 0 {
		return nil, status.Error(codes.PermissionDenied, "нет доступа к чату")
	}

	// Обнуляем last_message — удаление сообщений делает messaging-service
	c.db.ExecContext(ctx, "UPDATE chats  SET last_message=NULL, last_msg_sender=NULL WHERE id=$1", req.ChatId)
	c.db.ExecContext(ctx, "UPDATE groups SET last_message=NULL, last_msg_sender=NULL WHERE id=$1", req.ChatId)
	c.invalidateChat(ctx, req.ChatId, req.UserId)
	return &chatpb.ClearChatResponse{Ok: true}, nil
}

// ─── Groups ───────────────────────────────────────────────────────────────────

func (c *Chat) GetGroups(ctx context.Context, req *chatpb.GetGroupsRequest) (*chatpb.GetGroupsResponse, error) {
	key := groupsKeyPrefix + req.UserId
	if cached, err := c.cache.Get(ctx, key).Bytes(); err == nil {
		var resp chatpb.GetGroupsResponse
		if json.Unmarshal(cached, &resp) == nil {
			return &resp, nil
		}
	}

	rows, err := c.db.QueryContext(ctx, `
		SELECT g.id, g.name,
		       COALESCE(g.last_message, ''),
		       COALESCE(g.last_msg_sender::text, ''),
		       EXTRACT(EPOCH FROM COALESCE(g.updated_at, g.created_at))::INT
		FROM groups g
		JOIN group_members gm ON g.id = gm.group_id
		WHERE gm.user_id = $1
		ORDER BY COALESCE(g.updated_at, g.created_at) DESC`, req.UserId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer rows.Close()

	resp := &chatpb.GetGroupsResponse{}
	for rows.Next() {
		g := &chatpb.Group{}
		if err := rows.Scan(&g.Id, &g.Name, &g.LastMessage, &g.LastMsgSender, &g.UpdatedAt); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		resp.Groups = append(resp.Groups, g)
	}

	if data, err := json.Marshal(resp); err == nil {
		c.cache.Set(ctx, key, data, chatsCacheTTL)
	}
	return resp, nil
}

func (c *Chat) CreateGroup(ctx context.Context, req *chatpb.CreateGroupRequest) (*chatpb.CreateGroupResponse, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer tx.Rollback()

	var newID string
	if err = tx.QueryRowContext(ctx,
		"INSERT INTO conversations (type) VALUES ('group') RETURNING id",
	).Scan(&newID); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if _, err = tx.ExecContext(ctx,
		"INSERT INTO groups (id, name, admin_id, is_private) VALUES ($1, $2, $3, $4)",
		newID, req.Name, req.AdminId, req.IsPrivate,
	); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if _, err = tx.ExecContext(ctx,
		"INSERT INTO group_members (group_id, user_id, role) VALUES ($1, $2, 'admin')",
		newID, req.AdminId,
	); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// members — уже UUID (gateway резолвит username → id через user-service)
	for _, memberID := range req.Members {
		if memberID == req.AdminId {
			continue
		}
		if _, err = tx.ExecContext(ctx,
			"INSERT INTO group_members (group_id, user_id, role) VALUES ($1, $2, 'member')",
			newID, memberID,
		); err != nil {
			log.Printf("failed to add member %s: %v", memberID, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Инвалидируем кеш групп для всех участников
	cacheKeys := []string{groupsKeyPrefix + req.AdminId}
	for _, memberID := range req.Members {
		if memberID != req.AdminId {
			cacheKeys = append(cacheKeys, groupsKeyPrefix+memberID)
		}
	}
	c.cache.Del(ctx, cacheKeys...)
	return &chatpb.CreateGroupResponse{GroupId: newID}, nil
}

// ─── Participants ─────────────────────────────────────────────────────────────

func (c *Chat) GetParticipants(ctx context.Context, req *chatpb.GetParticipantsRequest) (*chatpb.GetParticipantsResponse, error) {
	key := participantsKeyPrefix + req.ChatId
	if cached, err := c.cache.Get(ctx, key).Result(); err == nil {
		var ids []string
		if json.Unmarshal([]byte(cached), &ids) == nil {
			return &chatpb.GetParticipantsResponse{UserIds: ids}, nil
		}
	}

	// Пробуем личный чат
	var u1, u2 string
	err := c.db.QueryRowContext(ctx,
		"SELECT user_id1, user_id2 FROM chats WHERE id = $1", req.ChatId,
	).Scan(&u1, &u2)
	if err == nil {
		ids := []string{u1, u2}
		if data, _ := json.Marshal(ids); data != nil {
			c.cache.Set(ctx, key, data, participantsCacheTTL)
		}
		return &chatpb.GetParticipantsResponse{UserIds: ids}, nil
	}

	// Группа
	rows, err := c.db.QueryContext(ctx,
		"SELECT user_id FROM group_members WHERE group_id = $1", req.ChatId,
	)
	if err != nil {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("chat not found: %s", req.ChatId))
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	if data, _ := json.Marshal(ids); data != nil {
		c.cache.Set(ctx, key, data, participantsCacheTTL)
	}
	return &chatpb.GetParticipantsResponse{UserIds: ids}, nil
}

// ─── UpdateLastMessage (вызывается messaging-service) ────────────────────────

func (c *Chat) UpdateLastMessage(ctx context.Context, req *chatpb.UpdateLastMessageRequest) (*chatpb.UpdateLastMessageResponse, error) {
	c.db.ExecContext(ctx,
		`UPDATE chats SET last_message=$1, last_msg_sender=$2::uuid, updated_at=NOW() WHERE id=$3`,
		req.Text, req.SenderId, req.ChatId,
	)
	c.db.ExecContext(ctx,
		`UPDATE groups SET last_message=$1, last_msg_sender=$2::uuid, updated_at=NOW() WHERE id=$3`,
		req.Text, req.SenderId, req.ChatId,
	)
	c.cache.Del(ctx, chatsKeyPrefix+req.SenderId, groupsKeyPrefix+req.SenderId)
	return &chatpb.UpdateLastMessageResponse{Ok: true}, nil
}

// ─── Инвалидация кеша ─────────────────────────────────────────────────────────

func (c *Chat) invalidateChat(ctx context.Context, chatID, userID string) {
	c.cache.Del(ctx,
		chatsKeyPrefix+userID,
		groupsKeyPrefix+userID,
		participantsKeyPrefix+chatID,
	)
}
