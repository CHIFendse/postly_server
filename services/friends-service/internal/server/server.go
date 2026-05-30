package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	friendspb "postly/proto/friends"
)

const wsPubPrefix = "ws:user:"

type Friends struct {
	friendspb.UnimplementedFriendsServiceServer
	db    *sql.DB
	cache *redis.Client
}

func New(db *sql.DB, cache *redis.Client) *Friends {
	return &Friends{db: db, cache: cache}
}

func (s *Friends) SendFriendRequest(ctx context.Context, req *friendspb.SendFriendRequestReq) (*friendspb.SendFriendRequestResp, error) {
	if req.SenderId == req.ReceiverId {
		return nil, status.Error(codes.InvalidArgument, "нельзя добавить себя в друзья")
	}

	u1, u2 := req.SenderId, req.ReceiverId
	if u1 > u2 {
		u1, u2 = u2, u1
	}
	var fid string
	if s.db.QueryRowContext(ctx, "SELECT id FROM friends WHERE user_id1=$1 AND user_id2=$2", u1, u2).Scan(&fid) == nil {
		return nil, status.Error(codes.AlreadyExists, "вы уже друзья")
	}

	var reqID string
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO friend_requests (sender_id, receiver_id)
		 VALUES ($1,$2)
		 ON CONFLICT (sender_id, receiver_id) DO UPDATE SET status='pending'
		 RETURNING id`,
		req.SenderId, req.ReceiverId,
	).Scan(&reqID)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Уведомляем получателя через Redis
	payload, _ := json.Marshal(map[string]string{
		"type":      "FRIEND_REQUEST",
		"sender_id": req.SenderId,
		"req_id":    reqID,
	})
	s.cache.Publish(ctx, wsPubPrefix+req.ReceiverId, payload)

	return &friendspb.SendFriendRequestResp{RequestId: reqID, ReceiverId: req.ReceiverId}, nil
}

func (s *Friends) GetFriendRequests(ctx context.Context, req *friendspb.GetFriendRequestsReq) (*friendspb.GetFriendRequestsResp, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, sender_id, EXTRACT(EPOCH FROM created_at)::INT
		FROM friend_requests
		WHERE receiver_id = $1 AND status = 'pending'
		ORDER BY created_at DESC`, req.UserId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer rows.Close()

	resp := &friendspb.GetFriendRequestsResp{}
	for rows.Next() {
		r := &friendspb.FriendRequest{}
		if err := rows.Scan(&r.Id, &r.SenderId, &r.CreatedAt); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		resp.Requests = append(resp.Requests, r)
	}
	return resp, nil
}

func (s *Friends) AcceptFriendRequest(ctx context.Context, req *friendspb.AcceptFriendRequestReq) (*friendspb.AcceptFriendRequestResp, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer tx.Rollback()

	var senderID string
	err = tx.QueryRowContext(ctx,
		`UPDATE friend_requests SET status='accepted'
		 WHERE id=$1 AND receiver_id=$2 AND status='pending'
		 RETURNING sender_id`,
		req.RequestId, req.UserId,
	).Scan(&senderID)
	if err != nil {
		return nil, status.Error(codes.NotFound, "заявка не найдена или уже обработана")
	}

	u1, u2 := senderID, req.UserId
	if u1 > u2 {
		u1, u2 = u2, u1
	}
	_, err = tx.ExecContext(ctx,
		"INSERT INTO friends (user_id1, user_id2) VALUES ($1,$2) ON CONFLICT DO NOTHING",
		u1, u2,
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if err = tx.Commit(); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Уведомляем отправителя что заявка принята
	payload, _ := json.Marshal(map[string]string{
		"type":        "FRIEND_ACCEPTED",
		"receiver_id": req.UserId,
	})
	s.cache.Publish(ctx, wsPubPrefix+senderID, payload)

	return &friendspb.AcceptFriendRequestResp{SenderId: senderID}, nil
}

func (s *Friends) DeclineFriendRequest(ctx context.Context, req *friendspb.DeclineFriendReq) (*friendspb.DeclineFriendResp, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE friend_requests SET status='declined'
		 WHERE id=$1 AND receiver_id=$2 AND status='pending'`,
		req.RequestId, req.UserId,
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("заявка не найдена"))
	}
	return &friendspb.DeclineFriendResp{Ok: true}, nil
}

func (s *Friends) GetFriends(ctx context.Context, req *friendspb.GetFriendsReq) (*friendspb.GetFriendsResp, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT CASE WHEN user_id1=$1 THEN user_id2 ELSE user_id1 END
		FROM friends
		WHERE user_id1=$1 OR user_id2=$1`, req.UserId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer rows.Close()

	resp := &friendspb.GetFriendsResp{Friends: []*friendspb.Friend{}}
	for rows.Next() {
		f := &friendspb.Friend{}
		if err := rows.Scan(&f.UserId); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		resp.Friends = append(resp.Friends, f)
	}
	return resp, nil
}
