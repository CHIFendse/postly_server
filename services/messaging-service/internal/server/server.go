package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	chatpb "postly/proto/chat"
	msgpb "postly/proto/messaging"
)

const (
	msgCachePrefix = "msg:messages:"
	msgCacheTTL    = 60 * time.Second
	wsPubPrefix    = "ws:user:"
)

type Messaging struct {
	msgpb.UnimplementedMessagingServiceServer
	db       *sql.DB
	cache    *redis.Client
	chatSvc  chatpb.ChatServiceClient
}

func New(db *sql.DB, cache *redis.Client, chatSvc chatpb.ChatServiceClient) *Messaging {
	return &Messaging{db: db, cache: cache, chatSvc: chatSvc}
}

func (s *Messaging) GetMessages(ctx context.Context, req *msgpb.GetMessagesRequest) (*msgpb.GetMessagesResponse, error) {
	key := msgCachePrefix + req.ChatId
	if cached, err := s.cache.Get(ctx, key).Bytes(); err == nil {
		var resp msgpb.GetMessagesResponse
		if json.Unmarshal(cached, &resp) == nil {
			return &resp, nil
		}
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, conversation_id, sender_id, text,
		       EXTRACT(EPOCH FROM created_at)::INT
		FROM messages
		WHERE conversation_id = $1
		ORDER BY created_at ASC`, req.ChatId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer rows.Close()

	resp := &msgpb.GetMessagesResponse{}
	for rows.Next() {
		m := &msgpb.Message{}
		if err := rows.Scan(&m.Id, &m.ChatId, &m.SenderId, &m.Text, &m.CreatedAt); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		resp.Messages = append(resp.Messages, m)
	}

	if data, err := json.Marshal(resp); err == nil {
		s.cache.Set(ctx, key, data, msgCacheTTL)
	}
	return resp, nil
}

func (s *Messaging) SendMessage(ctx context.Context, req *msgpb.SendMessageRequest) (*msgpb.SendMessageResponse, error) {
	var newID string
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO messages (conversation_id, sender_id, text) VALUES ($1, $2, $3) RETURNING id`,
		req.ChatId, req.SenderId, req.Text,
	).Scan(&newID)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	s.cache.Del(ctx, msgCachePrefix+req.ChatId)

	// Обновляем last_message в chat-service
	s.chatSvc.UpdateLastMessage(ctx, &chatpb.UpdateLastMessageRequest{
		ChatId:   req.ChatId,
		Text:     req.Text,
		SenderId: req.SenderId,
	})

	// Уведомляем участников через Redis pub/sub
	go s.notifyParticipants(req.ChatId, req.SenderId, newID, req.Text)

	return &msgpb.SendMessageResponse{MessageId: newID}, nil
}

func (s *Messaging) DeleteMessage(ctx context.Context, req *msgpb.DeleteMessageRequest) (*msgpb.DeleteMessageResponse, error) {
	var chatID string
	err := s.db.QueryRowContext(ctx,
		`DELETE FROM messages WHERE id=$1 AND sender_id=$2 RETURNING conversation_id`,
		req.MessageId, req.SenderId,
	).Scan(&chatID)
	if err != nil {
		return nil, status.Error(codes.NotFound, "сообщение не найдено или нет прав")
	}

	s.cache.Del(ctx, msgCachePrefix+chatID)

	// Обновляем last_message на новое последнее
	var lastText, lastSender string
	s.db.QueryRowContext(ctx,
		`SELECT text, sender_id FROM messages WHERE conversation_id=$1 ORDER BY created_at DESC LIMIT 1`,
		chatID,
	).Scan(&lastText, &lastSender)
	s.chatSvc.UpdateLastMessage(ctx, &chatpb.UpdateLastMessageRequest{
		ChatId:   chatID,
		Text:     lastText,
		SenderId: lastSender,
	})

	go s.notifyDeleteMessage(chatID, req.MessageId)

	return &msgpb.DeleteMessageResponse{ChatId: chatID}, nil
}

func (s *Messaging) DeleteChatMessages(ctx context.Context, req *msgpb.DeleteChatMsgsRequest) (*msgpb.DeleteChatMsgsResponse, error) {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM messages WHERE conversation_id=$1`, req.ChatId,
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	s.cache.Del(ctx, msgCachePrefix+req.ChatId)
	return &msgpb.DeleteChatMsgsResponse{Ok: true}, nil
}

// ─── Redis pub/sub broadcast ──────────────────────────────────────────────────

func (s *Messaging) notifyParticipants(chatID, senderID, msgID, text string) {
	ctx := context.Background()
	resp, err := s.chatSvc.GetParticipants(ctx, &chatpb.GetParticipantsRequest{ChatId: chatID})
	if err != nil {
		log.Printf("GetParticipants error: %v", err)
		return
	}
	payload, _ := json.Marshal(map[string]string{
		"type":      "NEW_MESSAGE",
		"chat_id":   chatID,
		"sender_id": senderID,
		"msg_id":    msgID,
		"text":      text,
	})
	for _, uid := range resp.UserIds {
		if uid != senderID { // отправитель получает подтверждение от WS-хендлера с username
			s.cache.Publish(ctx, wsPubPrefix+uid, payload)
		}
	}
}

func (s *Messaging) notifyDeleteMessage(chatID, msgID string) {
	ctx := context.Background()
	resp, err := s.chatSvc.GetParticipants(ctx, &chatpb.GetParticipantsRequest{ChatId: chatID})
	if err != nil {
		return
	}
	payload, _ := json.Marshal(map[string]string{
		"type":    "DELETE_MESSAGE",
		"chat_id": chatID,
		"msg_id":  msgID,
	})
	for _, uid := range resp.UserIds {
		s.cache.Publish(ctx, wsPubPrefix+uid, payload)
	}
}
