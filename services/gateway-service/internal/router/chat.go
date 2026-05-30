package router

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/redis/go-redis/v9"
	"gateway-service/internal/clients"
	chatpb "postly/proto/chat"
	userpb "postly/proto/user"
)

func RegisterChat(mux *http.ServeMux, c *clients.Clients, cache *redis.Client) {
	mux.HandleFunc("/getChats",    JWTMiddleware(c, handleGetChats(c)))
	mux.HandleFunc("/getGroups",   JWTMiddleware(c, handleGetGroups(c)))
	mux.HandleFunc("/createChat",  JWTMiddleware(c, handleCreateChat(c, cache)))
	mux.HandleFunc("/createGroup", JWTMiddleware(c, handleCreateGroup(c, cache)))
	mux.HandleFunc("/clearChat",   JWTMiddleware(c, handleClearChat(c)))
	mux.HandleFunc("/deleteChat",  JWTMiddleware(c, handleDeleteChat(c)))
}

func handleGetChats(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(UserIDKey).(string)

		resp, err := c.Chat.GetChats(r.Context(), &chatpb.GetChatsRequest{UserId: userID})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"message": "Ошибка получения чатов"})
			return
		}

		// name = UUID другого пользователя, last_msg_sender = UUID отправителя — резолвим оба
		type chatOut struct {
			Id          string `json:"id"`
			Name        string `json:"name"`
			LastMessage string `json:"last_message"`
			Username    string `json:"username"`
			UpdatedAt   int64  `json:"updated_at"`
		}

		// Кешируем resolved UUID → username в рамках запроса
		resolved := map[string]string{}
		resolve := func(uid string) string {
			if uid == "" {
				return ""
			}
			if v, ok := resolved[uid]; ok {
				return v
			}
			if u, err := c.User.GetUserByUserId(r.Context(), &userpb.GetUserByUserIdRequest{UserId: uid}); err == nil {
				resolved[uid] = u.Username
				return u.Username
			}
			return ""
		}

		out := make([]chatOut, 0, len(resp.Chats))
		for _, ch := range resp.Chats {
			out = append(out, chatOut{
				Id:          ch.Id,
				Name:        resolve(ch.Name),
				LastMessage: ch.LastMessage,
				Username:    resolve(ch.LastMsgSender),
				UpdatedAt:   ch.UpdatedAt,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}
}

func handleGetGroups(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(UserIDKey).(string)

		resp, err := c.Chat.GetGroups(r.Context(), &chatpb.GetGroupsRequest{UserId: userID})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"message": "Ошибка получения групп"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp.Groups)
	}
}

func handleCreateChat(c *clients.Clients, cache *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.Context().Value(UserIDKey).(string)

		var data struct {
			Username       string `json:"username"`
			TargetUsername string `json:"target_username"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		if data.TargetUsername != "" {
			data.Username = data.TargetUsername
		}

		targetResp, err := c.User.GetUserByUsername(r.Context(), &userpb.GetUserByUsernameRequest{Username: data.Username})
		if err != nil {
			log.Printf("createChat: GetUserByUsername(%q) err: %v", data.Username, err)
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"message": "Пользователь не найден"})
			return
		}

		resp, err := c.Chat.CreateChat(r.Context(), &chatpb.CreateChatRequest{
			UserId:         userID,
			TargetUsername: targetResp.UserId,
		})
		if err != nil {
			log.Printf("createChat: Chat.CreateChat err: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"message": "Ошибка создания чата"})
			return
		}

		// Уведомляем другого участника о новом чате
		go func() {
			payload, _ := json.Marshal(map[string]string{
				"type":    "NEW_CHAT",
				"chat_id": resp.ChatId,
			})
			cache.Publish(context.Background(), "ws:user:"+targetResp.UserId, payload)
		}()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func handleCreateGroup(c *clients.Clients, cache *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.Context().Value(UserIDKey).(string)

		var data struct {
			Name      string   `json:"name"`
			IsPrivate bool     `json:"is_private"`
			Members   []string `json:"members"` // usernames
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		memberIDs := []string{}
		for _, username := range data.Members {
			u, err := c.User.GetUserByUsername(r.Context(), &userpb.GetUserByUsernameRequest{Username: username})
			if err != nil {
				continue
			}
			memberIDs = append(memberIDs, u.UserId)
		}

		resp, err := c.Chat.CreateGroup(r.Context(), &chatpb.CreateGroupRequest{
			Name:      data.Name,
			AdminId:   userID,
			IsPrivate: data.IsPrivate,
			Members:   memberIDs,
		})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"message": "Ошибка создания группы"})
			return
		}

		// Уведомляем всех добавленных участников о новой группе
		go func(groupID string, ids []string) {
			payload, _ := json.Marshal(map[string]string{
				"type":    "NEW_CHAT",
				"chat_id": groupID,
			})
			for _, memberID := range ids {
				cache.Publish(context.Background(), "ws:user:"+memberID, payload)
			}
		}(resp.GroupId, memberIDs)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func handleClearChat(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.Context().Value(UserIDKey).(string)

		var data struct {
			ChatID string `json:"chat_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		_, err := c.Chat.ClearChat(r.Context(), &chatpb.ClearChatRequest{
			ChatId: data.ChatID,
			UserId: userID,
		})
		if err != nil {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"message": "Ошибка очистки чата"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"status": true})
	}
}

func handleDeleteChat(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.Context().Value(UserIDKey).(string)

		var data struct {
			ChatID string `json:"chat_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		_, err := c.Chat.DeleteChat(r.Context(), &chatpb.DeleteChatRequest{
			ChatId: data.ChatID,
			UserId: userID,
		})
		if err != nil {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"message": "Ошибка удаления чата"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"status": true})
	}
}
