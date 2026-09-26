package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	chatpb "postly/proto/chat"
	msgpb "postly/proto/messaging"
	s3pb "postly/proto/s3"
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
	s3Client s3pb.FileServiceClient
}

func New(db *sql.DB, cache *redis.Client, chatSvc chatpb.ChatServiceClient, s3Client s3pb.FileServiceClient) *Messaging {
	return &Messaging{db: db, cache: cache, chatSvc: chatSvc, s3Client: s3Client}
}

func (s *Messaging) GetMessages(
	ctx context.Context,
	req *msgpb.GetMessagesRequest,
) (*msgpb.GetMessagesResponse, error) {

	key := msgCachePrefix + req.ChatId

	if cached, err := s.cache.Get(ctx, key).Bytes(); err == nil {
		var resp msgpb.GetMessagesResponse

		if json.Unmarshal(cached, &resp) == nil {
			return &resp, nil
		}
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id,
		       chat_id,
		       sender_id,
		       text,
		       EXTRACT(EPOCH FROM created_at)::INT,
		       COALESCE(type, 'text'),
		       COALESCE(file_url, ''),
		       COALESCE(file_size::TEXT, ''),
		       COALESCE(file_name, '')
		FROM messages
		WHERE chat_id = $1
		ORDER BY created_at ASC
	`, req.ChatId)
	if err != nil {
		return nil, status.Error(
			codes.Internal,
			err.Error(),
		)
	}
	defer rows.Close()

	resp := &msgpb.GetMessagesResponse{}

	for rows.Next() {
		m := &msgpb.Message{}

		if err := rows.Scan(
			&m.Id,
			&m.ChatId,
			&m.SenderId,
			&m.Text,
			&m.CreatedAt,
			&m.Type,
			&m.FileUrl,
			&m.FileSize,
			&m.FileName,
		); err != nil {
			return nil, status.Error(
				codes.Internal,
				err.Error(),
			)
		}

		// В file_url остаётся S3-ключ: наружу файл отдаёт gateway
		// через /file с проверкой участника чата.

		resp.Messages = append(
			resp.Messages,
			m,
		)
	}

	if err := rows.Err(); err != nil {
		return nil, status.Error(
			codes.Internal,
			err.Error(),
		)
	}

	if data, err := json.Marshal(resp); err == nil {
		s.cache.Set(
			ctx,
			key,
			data,
			msgCacheTTL,
		)
	}

	return resp, nil
}

func (s *Messaging) SendMessage(ctx context.Context, req *msgpb.SendMessageRequest) (*msgpb.SendMessageResponse, error) {
	var newID string

	if req.Type == "" {
		req.Type = "text"
	}

	// Для файлов клиент уже загрузил объект в S3 по presigned-ссылке,
	// в FileUrl лежит S3-ключ — сохраняем его как есть.
	var fileURL, fileName, fileSize any
	if req.Type != "text" {
		if req.FileUrl == "" {
			return nil, status.Error(codes.InvalidArgument, "file url is required")
		}
		if req.FileName == "" {
			return nil, status.Error(codes.InvalidArgument, "file name is required")
		}
		fileURL = req.FileUrl
		fileName = req.FileName
		// Колонка file_size — integer; мусор/пусто сохраняем как NULL
		if n, err := strconv.ParseInt(req.FileSize, 10, 32); err == nil && n >= 0 {
			fileSize = n
		}
	}

	err := s.db.QueryRowContext(ctx,
		`INSERT INTO messages (chat_id, sender_id, text, type, file_url, file_size, file_name)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id`,
		req.ChatId,
		req.SenderId,
		req.Text,
		req.Type,
		fileURL,
		fileSize,
		fileName,
	).Scan(&newID)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	s.cache.Del(ctx, msgCachePrefix+req.ChatId)

	switch req.Type {
	case "image":
		req.Text = "Фотография..."
	case "video":
		req.Text = "Видео..."
	case "file":
		req.Text = "Файл..."
	case "voice":
		req.Text = "Голосовое сообщение..."
	}

	_, _ = s.chatSvc.UpdateLastMessage(ctx, &chatpb.UpdateLastMessageRequest{
		ChatId:   req.ChatId,
		Text:     req.Text,
		SenderId: req.SenderId,
	})

	return &msgpb.SendMessageResponse{MessageId: newID}, nil
}

func (s *Messaging) GetFileInfo(ctx context.Context, req *msgpb.GetFileInfoRequest) (*msgpb.GetFileInfoResponse, error) {
	resp := &msgpb.GetFileInfoResponse{}
	err := s.db.QueryRowContext(ctx,
		`SELECT chat_id, COALESCE(type, 'text'), COALESCE(file_url, ''), COALESCE(file_name, '')
		 FROM messages
		 WHERE id = $1`,
		req.MessageId,
	).Scan(&resp.ChatId, &resp.Type, &resp.S3Key, &resp.FileName)
	if err == sql.ErrNoRows {
		return nil, status.Error(codes.NotFound, "message not found")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if resp.S3Key == "" {
		return nil, status.Error(codes.NotFound, "message has no file")
	}
	return resp, nil
}

func (s *Messaging) DeleteMessage(ctx context.Context, req *msgpb.DeleteMessageRequest) (*msgpb.DeleteMessageResponse, error) {
	var chatID string
	var fileURL string
	var msgType string

	err := s.db.QueryRowContext(ctx,
		`DELETE FROM messages
		 WHERE id=$1 AND sender_id=$2
		 RETURNING chat_id, file_url, type`,
		req.MessageId,
		req.SenderId,
	).Scan(&chatID, &fileURL, &msgType)

	if err != nil {
		return nil, status.Error(codes.NotFound, "сообщение не найдено или нет прав")
	}

	// Удаляем файл из S3
	if fileURL != "" && msgType != "text" {
		_, err := s.s3Client.DeleteFile(ctx, &s3pb.DeleteFileRequest{
			S3Key: fileURL,
		})
		if err != nil {
			return nil, err
		}
	}

	s.cache.Del(ctx, msgCachePrefix+chatID)

	// дальше твой существующий код
	var lastText, lastSender, lastType string

	err = s.db.QueryRowContext(ctx,
		`SELECT text, sender_id, type
		 FROM messages
		 WHERE chat_id=$1
		 ORDER BY created_at DESC
		 LIMIT 1`,
		chatID,
	).Scan(&lastText, &lastSender, &lastType)

	if err == sql.ErrNoRows {
		lastText = ""
		lastSender = ""
	} else if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	switch lastType {
	case "image":
		lastText = "Фотография..."
	case "video":
		lastText = "Видео..."
	case "file":
		lastText = "Файл..."
	case "voice":
		lastText = "Голосовое сообщение..."
	}

	_, _ = s.chatSvc.UpdateLastMessage(ctx, &chatpb.UpdateLastMessageRequest{
		ChatId:   chatID,
		Text:     lastText,
		SenderId: lastSender,
	})

	go s.notifyDeleteMessage(chatID, req.MessageId, lastText, lastSender)

	return &msgpb.DeleteMessageResponse{ChatId: chatID}, nil
}

func (s *Messaging) DeleteChatMessages(ctx context.Context, req *msgpb.DeleteChatMsgsRequest) (*msgpb.DeleteChatMsgsResponse, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT file_url, type
		 FROM messages
		 WHERE chat_id=$1`,
		req.ChatId,
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer rows.Close()

	var files []string

	for rows.Next() {
		var fileURL string
		var msgType string

		if err := rows.Scan(&fileURL, &msgType); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		if fileURL != "" && msgType != "text" {
			files = append(files, fileURL)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Сначала удаляем файлы из S3
	for _, fileURL := range files {
		_, _ = s.s3Client.DeleteFile(ctx, &s3pb.DeleteFileRequest{
			S3Key: fileURL,
		})
	}

	// Потом удаляем сообщения из БД
	_, err = s.db.ExecContext(ctx,
		`DELETE FROM messages WHERE chat_id=$1`,
		req.ChatId,
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	s.cache.Del(ctx, msgCachePrefix+req.ChatId)

	return &msgpb.DeleteChatMsgsResponse{Ok: true}, nil
}

func (s *Messaging) notifyDeleteMessage(chatID, msgID, lastMessage, lastSenderID string) {
	ctx := context.Background()
	resp, err := s.chatSvc.GetParticipants(ctx, &chatpb.GetParticipantsRequest{ChatId: chatID})
	if err != nil {
		return
	}
	payload, _ := json.Marshal(map[string]string{
		"type":         "DELETE_MESSAGE",
		"chat_id":      chatID,
		"message_id":   msgID,
		"last_message": lastMessage,
		"sender_id":    lastSenderID,
	})
	for _, uid := range resp.UserIds {
		s.cache.Publish(ctx, wsPubPrefix+uid, payload)
	}
}
