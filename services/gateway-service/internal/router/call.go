package router

import (
	"context"
	"encoding/json"
	"log"
	"slices"
	"time"

	"github.com/redis/go-redis/v9"

	"gateway-service/internal/clients"
	chatpb "postly/proto/chat"
	userpb "postly/proto/user"
)

var (
	sfuCallTypes = map[string]bool{
		"CALL_JOIN":   true,
		"CALL_ANSWER": true,
		"CALL_ICE":    true,
		"CALL_HANGUP": true,
	}
	ringCallTypes = map[string]bool{
		"CALL_INVITE": true,
		"CALL_ACCEPT": true,
		"CALL_REJECT": true,
		"CALL_HANGUP": true,
	}
)

func handleCall(ctx context.Context, msg map[string]string, senderID string, c *clients.Clients, cache *redis.Client) {
	chatID, typ := msg["chat_id"], msg["type"]
	if !sfuCallTypes[typ] && !ringCallTypes[typ] {
		return
	}

	pts, err := c.Chat.GetParticipants(ctx, &chatpb.GetParticipantsRequest{ChatId: chatID})
	if err != nil {
		log.Printf("[GW] call GetParticipants: %v", err)
		return
	}
	if !slices.Contains(pts.UserIds, senderID) {
		log.Printf("[GW] user=%s not in chat=%s, drop %s", senderID, chatID, typ)
		return
	}

	if typ == "CALL_INVITE" {
		ok, err := cache.SetNX(ctx, "callinvite:"+senderID+":"+chatID, 1, 5*time.Second).Result()
		if err == nil && !ok {
			return
		}
	}

	if sfuCallTypes[typ] {
		payload, _ := json.Marshal(map[string]string{
			"type":      typ,
			"chat_id":   chatID,
			"user_id":   senderID,
			"sdp":       msg["sdp"],
			"candidate": msg["candidate"],
		})
		if err := cache.Publish(ctx, "callsfu:signal", payload).Err(); err != nil {
			log.Printf("[GW] publish callsfu: %v", err)
		}
	}

	if !ringCallTypes[typ] {
		return
	}

	name := ""
	if u, err := c.User.GetUserByUserId(ctx, &userpb.GetUserByUserIdRequest{UserId: senderID}); err == nil {
		name = u.Username
	}
	kind := "direct"
	if len(pts.UserIds) > 2 {
		kind = "group"
	}

	payload, _ := json.Marshal(map[string]string{
		"type":      typ,
		"chat_id":   chatID,
		"sender_id": senderID,
		"name":      name,
		"kind":      kind,
	})
	for _, uid := range pts.UserIds {
		if uid == senderID {
			continue
		}
		cache.Publish(ctx, "ws:user:"+uid, payload)
		if typ == "CALL_INVITE" {
			go sendCallPush(ctx, cache, uid, name, chatID)
		}
	}
}
