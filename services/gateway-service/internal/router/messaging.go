package router

import (
	"encoding/json"
	"net/http"

	"gateway-service/internal/clients"
	msgpb "postly/proto/messaging"
	userpb "postly/proto/user"
)

func RegisterMessaging(mux *http.ServeMux, c *clients.Clients) {
	mux.HandleFunc("/getMessages",   JWTMiddleware(c, handleGetMessages(c)))
	mux.HandleFunc("/sendMessage",   JWTMiddleware(c, handleSendMessage(c)))
	mux.HandleFunc("/deleteMessage", JWTMiddleware(c, handleDeleteMessage(c)))
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

		// Собираем ответ с username
		type msgOut struct {
			Id        string `json:"id"`
			ChatId    string `json:"chat_id"`
			SenderId  string `json:"sender_id"`
			Text      string `json:"text"`
			CreatedAt int64  `json:"created_at"`
			Username  string `json:"username"`
		}
		out := make([]msgOut, 0, len(resp.Messages))
		for _, m := range resp.Messages {
			out = append(out, msgOut{
				Id:        m.Id,
				ChatId:    m.ChatId,
				SenderId:  m.SenderId,
				Text:      m.Text,
				CreatedAt: m.CreatedAt,
				Username:  usernames[m.SenderId],
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
