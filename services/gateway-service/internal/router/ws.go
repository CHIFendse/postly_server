package router

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"

	"gateway-service/internal/clients"
	authpb "postly/proto/auth"
	chatpb "postly/proto/chat"
	msgpb "postly/proto/messaging"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func RegisterWS(mux *http.ServeMux, c *clients.Clients, cache *redis.Client) {
	mux.HandleFunc("/ws", handleWS(c, cache))
}

func handleWS(c *clients.Clients, cache *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.URL.Query().Get("token"), "Bearer ")
		if token == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		resp, err := c.Auth.ValidateToken(r.Context(), &authpb.ValidateTokenRequest{Token: token})
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

		sub := cache.Subscribe(r.Context(), "ws:user:"+userID)
		defer sub.Close()
		ch := sub.Channel()

		go func() {
			for {
				_, raw, err := conn.ReadMessage()
				if err != nil {
					sub.Close()
					return
				}
				go handleIncoming(raw, userID, c.Messaging, c.Chat, cache)
			}
		}()

		for msg := range ch {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload)); err != nil {
				log.Printf("WS write error user %s: %v", userID, err)
				return
			}
		}
	}
}

func handleIncoming(raw []byte, senderID string, msgSvc msgpb.MessagingServiceClient, chatSvc chatpb.ChatServiceClient, cache *redis.Client) {
	var msg map[string]string
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}
	ctx := context.Background()
	chatID := msg["chat_id"]
	if chatID == "" {
		return
	}

	// TYPING
	if msg["type"] == "TYPING" {
		pts, err := chatSvc.GetParticipants(ctx, &chatpb.GetParticipantsRequest{ChatId: chatID})
		if err != nil {
			return
		}
		payload, _ := json.Marshal(map[string]string{
			"type":      "TYPING",
			"chat_id":   chatID,
			"sender_id": senderID,
			"username":  msg["username"],
		})
		for _, uid := range pts.UserIds {
			if uid != senderID {
				cache.Publish(ctx, "ws:user:"+uid, payload)
			}
		}
		return
	}

	// WebRTC signaling → SFU (not relayed to the other client directly).
	// The SFU creates a peer connection per client and forwards audio between them.
	if msg["type"] == "CALL_OFFER" || msg["type"] == "CALL_ICE" {
		log.Printf("[GW] routing %s user=%s chat=%s", msg["type"], senderID, chatID)
		sfuPayload, _ := json.Marshal(map[string]string{
			"type":      msg["type"],
			"chat_id":   chatID,
			"user_id":   senderID,
			"sdp":       msg["sdp"],
			"candidate": msg["candidate"],
		})
		if err := cache.Publish(ctx, "callsfu:signal", string(sfuPayload)).Err(); err != nil {
			log.Printf("[GW] Redis publish error: %v", err)
		}
		return
	}

	log.Printf("[GW] msg type=%s user=%s chat=%s", msg["type"], senderID, chatID)

	// Call control — relay to all other participants
	callTypes := map[string]bool{
		"CALL_INVITE": true,
		"CALL_ACCEPT": true,
		"CALL_REJECT": true,
		"CALL_HANGUP": true,
	}
	if callTypes[msg["type"]] {
		pts, err := chatSvc.GetParticipants(ctx, &chatpb.GetParticipantsRequest{ChatId: chatID})
		if err != nil {
			log.Printf("WS call GetParticipants: %v", err)
			return
		}
		payload, _ := json.Marshal(msg)
		for _, uid := range pts.UserIds {
			if uid == senderID {
				continue
			}
			cache.Publish(ctx, "ws:user:"+uid, payload)
			if msg["type"] == "CALL_INVITE" {
				go sendCallPush(ctx, cache, uid, msg["name"], chatID)
			}
		}
		return
	}

	// Text message
	text := msg["text"]
	if text == "" {
		return
	}
	sendResp, err := msgSvc.SendMessage(ctx, &msgpb.SendMessageRequest{
		ChatId:   chatID,
		SenderId: senderID,
		Text:     text,
	})
	if err != nil {
		log.Printf("WS SendMessage: %v", err)
		return
	}

	pts, err := chatSvc.GetParticipants(ctx, &chatpb.GetParticipantsRequest{ChatId: chatID})
	if err != nil {
		log.Printf("WS GetParticipants: %v", err)
		confirm, _ := json.Marshal(map[string]string{
			"type": "NEW_MESSAGE", "id": sendResp.MessageId,
			"chat_id": chatID, "sender_id": senderID,
			"username": msg["username"], "text": text,
		})
		cache.Publish(ctx, "ws:user:"+senderID, confirm)
		return
	}

	payload, _ := json.Marshal(map[string]string{
		"type":       "NEW_MESSAGE",
		"id":         sendResp.MessageId,
		"chat_id":    chatID,
		"sender_id":  senderID,
		"username":   msg["username"],
		"text":       text,
		"created_at": time.Now().UTC().Format(time.RFC3339),
	})
	for _, uid := range pts.UserIds {
		cache.Publish(ctx, "ws:user:"+uid, payload)
		if uid != senderID {
			go sendMessagePush(ctx, cache, uid, msg["username"], text, chatID)
		}
	}
}
