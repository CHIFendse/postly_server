package server

import (
	"database/sql"
	"context"
	"github.com/google/uuid"
	"log"
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