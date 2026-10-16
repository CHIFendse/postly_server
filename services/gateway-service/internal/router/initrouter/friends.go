package initrouter

import (
	"context"
	"fmt"
	"gateway-service/internal/clients"
	"gateway-service/internal/router/middlewares"
	"net/url"

	friendspb "postly/proto/friends"
	userpb "postly/proto/user"
)

type friendOut struct {
	Id        string `json:"id"`
	Username  string `json:"username"`
	AvatarURL string `json:"avatar_url"`
}

type reqOut struct {
	Id         string `json:"id"`
	SenderId   string `json:"sender_id"`
	Username   string `json:"username"`
	AvatarURL  string `json:"avatar_url"`
	CreatedAt  int64  `json:"created_at"`
	ReceiverId string `json:"receiver_id"`
}

func GetUser(ctx context.Context, senderId string, c *clients.Clients) (string, error) {
	u, err := c.User.GetUserByUserId(ctx, &userpb.GetUserByUserIdRequest{UserId: senderId})
	if err != nil {
		return "", err
	}
	return u.Username, nil
}

func resolve(ctx context.Context, c *clients.Clients, resolved map[string]string, uid string) string {
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

func avatarURLForUser(userID string) string {
	if userID == "" {
		return ""
	}
	return "/avatar?user_id=" + url.QueryEscape(userID)
}

func GetFriendRequests(ctx context.Context, c *clients.Clients, userID string) ([]reqOut, error) {
	resp, err := c.Friends.GetFriendRequests(ctx, &friendspb.GetFriendRequestsReq{UserId: userID})
	if err != nil {
		return nil, err
	}

	resolved := map[string]string{}
	out := make([]reqOut, 0, len(resp.Requests))
	for _, req := range resp.Requests {
		chatUser := resolve(ctx, c, resolved, req.SenderId)
		avatarURL := avatarURLForUser(req.SenderId)
		out = append(out, reqOut{
			Id:        req.Id,
			SenderId:  req.SenderId,
			Username:  chatUser,
			AvatarURL: avatarURL,
			CreatedAt: req.CreatedAt,
		})
	}

	return out, nil
}

func GetFriends(ctx context.Context, c *clients.Clients, userID string) ([]friendOut, error) {

	resp, err := c.Friends.GetFriends(ctx, &friendspb.GetFriendsReq{UserId: userID})
	if err != nil {
		return nil, err
	}

	out := make([]friendOut, 0, len(resp.Friends))
	resolved := make(map[string]string)
	for _, f := range resp.Friends {
		chatUser := resolve(ctx, c, resolved, f.UserId)
		avatarURL := avatarURLForUser(f.UserId)
		out = append(out, friendOut{Id: f.UserId, Username: chatUser, AvatarURL: avatarURL})
	}

	return out, nil
}

func GetSentRequests(ctx context.Context, c *clients.Clients, userID string) ([]reqOut, error) {

	resp, err := c.Friends.GetSentRequests(ctx, &friendspb.GetFriendRequestsReq{UserId: userID})
	if err != nil {
		return nil, err
	}

	resolved := make(map[string]string)
	out := make([]reqOut, 0, len(resp.Requests))
	for _, req := range resp.Requests {
		chatUser := resolve(ctx, c, resolved, req.SenderId)
		avatarURL := avatarURLForUser(req.SenderId)
		out = append(out, reqOut{
			Id:         req.Id,
			ReceiverId: req.SenderId,
			Username:   chatUser,
			AvatarURL:  avatarURL,
			CreatedAt:  req.CreatedAt,
		})
	}

	return out, nil
}
