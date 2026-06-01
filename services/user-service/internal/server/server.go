package server

import (
	"database/sql"
	"context"
	"github.com/google/uuid"
	"log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	userpb "postly/proto/user"

)
type User struct {
	userpb.UnimplementedUserServiceServer
	db    *sql.DB
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
