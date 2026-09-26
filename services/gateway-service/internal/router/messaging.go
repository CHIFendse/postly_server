package router

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"gateway-service/internal/clients"
	msgpb "postly/proto/messaging"
	s3pb "postly/proto/s3"
	userpb "postly/proto/user"
)


func RegisterMessaging(mux *http.ServeMux, c *clients.Clients) {
	mux.HandleFunc("/getMessages",   JWTMiddleware(c, handleGetMessages(c)))
	mux.HandleFunc("/sendMessage",   JWTMiddleware(c, handleSendMessage(c)))
	mux.HandleFunc("/deleteMessage", JWTMiddleware(c, handleDeleteMessage(c)))
	mux.HandleFunc("/getUploadUrl",  JWTMiddleware(c, handleGetUploadURL(c)))
}

// Папка в бакете по типу сообщения, а если он не передан — по content-type
var folderByMsgType = map[string]string{
	"image": "images",
	"video": "videos",
	"voice": "voices",
	"file":  "files",
}

func uploadFolder(msgType, contentType string) string {
	if f, ok := folderByMsgType[msgType]; ok {
		return f
	}
	switch {
	case strings.HasPrefix(contentType, "image/"):
		return "images"
	case strings.HasPrefix(contentType, "video/"):
		return "videos"
	case strings.HasPrefix(contentType, "audio/"):
		return "voices"
	}
	return "files"
}

// ownFileKey проверяет, что ключ выдан этому пользователю через /getUploadUrl:
// {folder}/{userID}/{name}
func ownFileKey(key, userID string) bool {
	parts := strings.Split(key, "/")
	if len(parts) != 3 || parts[1] != userID || parts[2] == "" {
		return false
	}
	for _, f := range folderByMsgType {
		if parts[0] == f {
			return true
		}
	}
	return false
}

func handleGetUploadURL(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.Context().Value(UserIDKey).(string)

		var data struct {
			FileName    string `json:"file_name"`
			ContentType string `json:"content_type"`
			// Клиент может прислать число или строку — пока не используется
			FileSize json.RawMessage `json:"file_size"`
			// Необязательно: тип сообщения (image/video/voice/file)
			MessageType string `json:"message_type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			log.Printf("[GW] getUploadUrl bad body from %s: %v", userID, err)
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		resp, err := c.S3.GetUploadUrl(r.Context(), &s3pb.GetUploadUrlRequest{
			UserId:      userID,
			FileName:    data.FileName,
			ContentType: data.ContentType,
			Folder:      uploadFolder(data.MessageType, data.ContentType),
		})
		if err != nil {
			log.Printf("[GW] GetUploadUrl: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"message": "Ошибка получения ссылки для загрузки"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"upload_url": resp.UploadUrl,
			"s3_key":     resp.S3Key,
		})
	}
}

func handleGetMessages(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Поддержка и GET ?chat_id=... и POST {chat_id: ...}
		chatID := r.URL.Query().Get("chat_id")
		if chatID == "" {
			var body struct {
				ChatID string `json:"chat_id"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			chatID = body.ChatID
		}
		if chatID == "" {
			http.Error(w, "chat_id required", http.StatusBadRequest)
			return
		}

		resp, err := c.Messaging.GetMessages(r.Context(), &msgpb.GetMessagesRequest{ChatId: chatID})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"message": "Ошибка получения сообщений"})
			return
		}

		// Резолвим sender_id → username для каждого уникального отправителя
		usernames := map[string]string{}
		for _, m := range resp.Messages {
			if _, ok := usernames[m.SenderId]; !ok {
				u, err := c.User.GetUserByUserId(r.Context(), &userpb.GetUserByUserIdRequest{UserId: m.SenderId})
				if err == nil {
					usernames[m.SenderId] = u.Username
				}
			}
		}

		// Собираем ответ с username. created_at → миллисекунды (клиент ждёт ms для new Date())
		type msgOut struct {
			Id        string `json:"id"`
			ChatId    string `json:"chat_id"`
			SenderId  string `json:"sender_id"`
			Text      string `json:"text"`
			CreatedAt int64  `json:"created_at"`
			Username  string `json:"username"`
			Type      string `json:"message_type"`
			FileURL   string `json:"file_url"`
			FileName  string `json:"file_name"`
			FileSize  string `json:"file_size"`
		}
		out := make([]msgOut, 0, len(resp.Messages))
		for _, m := range resp.Messages {
			msgType := m.Type
			if msgType == "" {
				msgType = "text"
			}

			// В базе S3-ключ — клиенту отдаём адрес /file с проверкой доступа
			fileURL := ""
			if msgType != "text" && m.FileUrl != "" {
				fileURL = fileURLFor(m.Id)
			}

			out = append(out, msgOut{
				Id:        m.Id,
				ChatId:    m.ChatId,
				SenderId:  m.SenderId,
				Text:      m.Text,
				CreatedAt: m.CreatedAt * 1000, // секунды → миллисекунды
				Username:  usernames[m.SenderId],
				Type:      msgType,
				FileURL:   fileURL,
				FileName:  m.FileName,
				FileSize:  m.FileSize,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}
}

func handleSendMessage(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.Context().Value(UserIDKey).(string)

		var data struct {
			ChatID string `json:"chat_id"`
			Text   string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		resp, err := c.Messaging.SendMessage(r.Context(), &msgpb.SendMessageRequest{
			ChatId:   data.ChatID,
			SenderId: userID,
			Text:     data.Text,
		})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"message": "Ошибка отправки сообщения"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": resp.MessageId})
	}
}

func handleDeleteMessage(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.Context().Value(UserIDKey).(string)

		var data struct {
			MessageID string `json:"message_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		resp, err := c.Messaging.DeleteMessage(r.Context(), &msgpb.DeleteMessageRequest{
			MessageId: data.MessageID,
			SenderId:  userID,
		})
		if err != nil {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"message": "Ошибка удаления сообщения"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"chat_id": resp.ChatId})
	}
}
