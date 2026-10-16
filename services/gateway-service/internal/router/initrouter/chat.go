package initrouter

import (
	"context"
	"fmt"
	"gateway-service/internal/clients"
	"gateway-service/internal/router/middlewares"

	chatpb "postly/proto/chat"
)

type Chats struct {
	Id          string `json:"id"`
	UserID      string `json:"user_id"`
	Name        string `json:"name"`
	LastMessage string `json:"last_message"`
	Username    string `json:"username"`
	AvatarURL   string `json:"avatar_url"`
	UpdatedAt   int64  `json:"updated_at"`
}

type userOut struct {
	username  string
	avatarURL string
}

type Groups struct {
	Id          string `json:"id"`
	Name        string `json:"name"`
	LastMessage string `json:"last_message"`
	Username    string `json:"username"`
	UpdatedAt   int64  `json:"updated_at"`
}

func GetChats(ctx context.Context, c *clients.Clients, userID string) ([]Chats, error) {
	resp, err := c.Chat.GetChats(ctx, &chatpb.GetChatsRequest{UserId: userID})
	if err != nil {
		return nil, err
	}

	resolved := map[string]userOut{}
	resolve := func(uid string) userOut {
		if uid == "" {
			return userOut{}
		}
		if v, ok := resolved[uid]; ok {
			return v
		}
		username, err := GetUser(ctx, uid, c)
		if err != nil {
			fmt.Printf("[ERROR] ID: %v, err: %v\n", ctx.Value(middlewares.RequestIDKey), err)
			return userOut{username: uid}
		}
		result := userOut{username: username, avatarURL: avatarURLForUser(uid)}
		resolved[uid] = result
		return result
	}

	out := make([]Chats, 0, len(resp.Chats))
	for _, ch := range resp.Chats {
		chatUser := resolve(ch.Name)
		lastMessageUser := resolve(ch.LastMsgSender)
		out = append(out, Chats{
			Id:          ch.Id,
			UserID:      ch.Name,
			Name:        chatUser.username,
			LastMessage: ch.LastMessage,
			Username:    lastMessageUser.username,
			AvatarURL:   chatUser.avatarURL,
			UpdatedAt:   ch.UpdatedAt,
		})
	}

	return out, nil
}

func GetGroups(ctx context.Context, c *clients.Clients, userID string) ([]Groups, error) {
	resp, err := c.Chat.GetGroups(ctx, &chatpb.GetGroupsRequest{UserId: userID})
	if err != nil {
		return nil, err
	}

	resolved := map[string]string{}
	resolve := func(uid string) string {
		if uid == "" {
			return ""
		}
		if v, ok := resolved[uid]; ok {
			return v
		}
		username, err := GetUser(ctx, uid, c)
		if err != nil {
			fmt.Printf("[ERROR] ID: %v, err: %v\n", ctx.Value(middlewares.RequestIDKey), err)
			return uid
		}
		resolved[uid] = username
		return username
	}

	out := make([]Groups, 0, len(resp.Groups))
	for _, g := range resp.Groups {
		out = append(out, Groups{
			Id:          g.Id,
			Name:        g.Name,
			LastMessage: g.LastMessage,
			Username:    resolve(g.LastMsgSender),
			UpdatedAt:   g.UpdatedAt,
		})
	}
	return out, nil
}
