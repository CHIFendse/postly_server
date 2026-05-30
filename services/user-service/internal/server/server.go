package server

import (
	"database/sql"
	"context"
	"github.com/google/uuid"
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
	u.db.ExecContext(ctx,
		`INSERT INTO users (user_id, username, email, phone) VALUES ($1, $2, $3, $4)`,
		id, req.Username, req.Email, req.Phone,
	)
	return &userpb.RegisterResponse{UserId: id}, nil
}