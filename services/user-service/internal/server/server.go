package server

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	userpb "postly/proto/user"
)

type User struct {
	userpb.UnimplementedUserServiceServer
	db *sql.DB
}

func New(db *sql.DB) *User {
	return &User{db: db}
}

func (u *User) Register(ctx context.Context, req *userpb.RegisterRequest) (*userpb.RegisterResponse, error) {
	var id string
	id = uuid.New().String()
	_, err := u.db.ExecContext(ctx,
		`INSERT INTO users (user_id, username, email, phone) VALUES ($1, $2, $3, $4)`,
		id, req.Username, req.Email, req.Phone,
	)
	if err != nil {
		log.Printf("Register error: %v", err)
		return nil, err
	}
	return &userpb.RegisterResponse{UserId: id}, nil
}

func (u *User) DeleteUser(ctx context.Context, req *userpb.DeleteUserRequest) (*userpb.DeleteUserResponse, error) {
	_, err := u.db.ExecContext(ctx, `DELETE FROM users WHERE user_id = $1`, req.UserId)
	if err != nil {
		return nil, status.Error(codes.Internal, "db error")
	}
	return &userpb.DeleteUserResponse{Ok: true}, nil
}

func (u *User) GetUserByUsername(ctx context.Context, req *userpb.GetUserByUsernameRequest) (*userpb.GetUserByUsernameResponse, error) {
	var id string
	err := u.db.QueryRowContext(ctx,
		`SELECT user_id FROM users WHERE LOWER(username) = LOWER($1)`,
		req.Username,
	).Scan(&id)
	if err != nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}
	return &userpb.GetUserByUsernameResponse{UserId: id}, nil
}

func (u *User) GetUserByUserId(ctx context.Context, req *userpb.GetUserByUserIdRequest) (*userpb.GetUserByUserIdResponse, error) {
	var username string
	err := u.db.QueryRowContext(ctx,
		`SELECT username FROM users WHERE user_id = $1`,
		req.UserId,
	).Scan(&username)
	if err != nil {
		return nil, status.Error(codes.NotFound, "user not found")
	}
	return &userpb.GetUserByUserIdResponse{UserId: req.UserId, Username: username}, nil
}

func (u *User) SetAvatar(ctx context.Context, req *userpb.SetAvatarRequest) (*userpb.SetAvatarResponse, error) {
	if req.UserId == "" || req.S3Key == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id and s3_key are required")
	}
	if !strings.HasPrefix(req.S3Key, "avatars/"+req.UserId+"/") {
		return nil, status.Error(codes.InvalidArgument, "s3_key must start with 'avatars/"+req.UserId+"/'")
	}
	_, err := u.db.ExecContext(ctx,
		"INSERT INTO user_avatars (user_id, s3_key) VALUES ($1, $2)", req.UserId, req.S3Key)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) {
			switch pqErr.Code {
			case "23503":
				return nil, status.Error(codes.NotFound, "user not found")
			case "23505":
				return nil, status.Error(codes.AlreadyExists, "avatar already exists")
			}
		}
		log.Printf("SetAvatar user=%s: %v", req.UserId, err)
		return nil, status.Error(codes.Internal, "db error")
	}
	return &userpb.SetAvatarResponse{}, nil
}

func (u *User) DeleteAvatar(ctx context.Context, req *userpb.DeleteAvatarRequest) (*userpb.DeleteAvatarResponse, error) {
	if req.UserId == "" || req.S3Key == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id and s3_key are required")
	}
	res, err := u.db.ExecContext(ctx,
		"DELETE FROM user_avatars WHERE user_id = $1 AND s3_key = $2", req.UserId, req.S3Key)
	if err != nil {
		log.Printf("DeleteAvatar user=%s: %v", req.UserId, err)
		return nil, status.Error(codes.Internal, "db error")
	}
	affected, err := res.RowsAffected()
	if err != nil {
		log.Printf("DeleteAvatar user=%s: %v", req.UserId, err)
		return nil, status.Error(codes.Internal, "db error")
	}
	if affected == 0 {
		log.Printf("DeleteAvatar user=%s: avatar not found", req.UserId)
		return nil, status.Error(codes.NotFound, "avatar not found")
	}

	return &userpb.DeleteAvatarResponse{}, nil
}

func (u *User) GetAvatars(ctx context.Context, req *userpb.GetAvatarsRequest) (*userpb.GetAvatarsResponse, error) {
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	rows, err := u.db.QueryContext(ctx,
		"SELECT s3_key, created_at FROM user_avatars WHERE user_id = $1 ORDER BY created_at DESC", req.UserId)
	if err != nil {
		log.Printf("GetAvatars user=%s: %v", req.UserId, err)
		return nil, status.Error(codes.Internal, "db error")
	}
	defer rows.Close()

	var avatars []*userpb.Avatar
	for rows.Next() {
		var s3Key string
		var createdAt time.Time
		if err := rows.Scan(&s3Key, &createdAt); err != nil {
			log.Printf("GetAvatars user=%s: %v", req.UserId, err)
			return nil, status.Error(codes.Internal, "db error")
		}
		avatars = append(avatars, &userpb.Avatar{
			S3Key:     s3Key,
			CreatedAt: createdAt.Unix(),
		})
	}
	if rows.Err() != nil {
		log.Printf("GetAvatars user=%s: %v", req.UserId, rows.Err())
		return nil, status.Error(codes.Internal, "db error")
	}
	return &userpb.GetAvatarsResponse{Avatars: avatars}, nil
}

func (u *User) GetUserInfo(ctx context.Context, req *userpb.GetUserInfoRequest) (*userpb.GetUserInfoResponse, error) {
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	var username string
	var email string
	var phone string
	var s3Key string
	err := u.db.QueryRowContext(ctx,
		`SELECT u.user_id, u.username, u.email, u.phone,
		       COALESCE((SELECT a.s3_key FROM user_avatars a
		                 WHERE a.user_id = u.user_id
		                 ORDER BY a.created_at DESC LIMIT 1), '')
		FROM users u
		WHERE u.user_id = $1`,
		req.UserId,
	).Scan(&req.UserId, &username, &email, &phone, &s3Key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, status.Error(codes.NotFound, "user not found")
	} else if err != nil {
		log.Printf("GetUserInfo user=%s: %v", req.UserId, err)
		return nil, status.Error(codes.Internal, "db error")
	}
	return &userpb.GetUserInfoResponse{
		UserId:   req.UserId,
		Username: username,
		Email:    email,
		Phone:    phone,
		S3Key:    s3Key,
	}, nil
}
