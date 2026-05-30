package router

import (
	"encoding/json"
	"net/http"

	"gateway-service/internal/clients"
	msgpb "postly/proto/messaging"
)

func RegisterMessaging(mux *http.ServeMux, c *clients.Clients) {
	mux.HandleFunc("/getMessages",   JWTMiddleware(c, handleGetMessages(c)))
	mux.HandleFunc("/sendMessage",   JWTMiddleware(c, handleSendMessage(c)))
	mux.HandleFunc("/deleteMessage", JWTMiddleware(c, handleDeleteMessage(c)))
}

func handleGetMessages(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		chatID := r.URL.Query().Get("chat_id")
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

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp.Messages)
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
