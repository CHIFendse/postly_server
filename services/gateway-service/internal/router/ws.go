package router

import (
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	authpb "postly/proto/auth"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func RegisterWS(mux *http.ServeMux, auth authpb.AuthServiceClient, cache *redis.Client) {
	mux.HandleFunc("/ws", handleWS(auth, cache))
}

func handleWS(authSvc authpb.AuthServiceClient, cache *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.URL.Query().Get("token"), "Bearer ")
		if token == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		resp, err := authSvc.ValidateToken(r.Context(), &authpb.ValidateTokenRequest{Token: token})
		if err != nil || !resp.Valid {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		userID := resp.UserId

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WS upgrade: %v", err)
			return
		}
		defer conn.Close()

		// Подписываемся на Redis канал пользователя
		ctx := r.Context()
		sub := cache.Subscribe(ctx, "ws:user:"+userID)
		defer sub.Close()
		ch := sub.Channel()

		// Горутина: читаем входящие WS-сообщения (typing и т.д.)
		go func() {
			for {
				_, _, err := conn.ReadMessage()
				if err != nil {
					sub.Close()
					return
				}
			}
		}()

		// Пересылаем Redis-события клиенту
		for msg := range ch {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload)); err != nil {
				log.Printf("WS write error user %s: %v", userID, err)
				return
			}
		}
	}
}
