package server

import (
	"context"
	"database/sql"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"auth-service/internal/jwt"
	authpb "postly/proto/auth"
)

const tokenCachePrefix = "auth:token:"
const tokenCacheTTL = 5 * time.Minute

type Auth struct {
	authpb.UnimplementedAuthServiceServer
	db    *sql.DB
	cache *redis.Client
}

func New(db *sql.DB, cache *redis.Client) *Auth {
	return &Auth{db: db, cache: cache}
}

// SetCredentials вызывается user-service после создания пользователя.
func (s *Auth) SetCredentials(ctx context.Context, req *authpb.SetCredentialsRequest) (*authpb.SetCredentialsResponse, error) {
	if req.UserId == "" || req.Password == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id and password required")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to hash password")
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO credentials (user_id, password_hash) VALUES ($1, $2)
		 ON CONFLICT (user_id) DO UPDATE SET password_hash = EXCLUDED.password_hash`,
		req.UserId, string(hash),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, "db error")
	}

	return &authpb.SetCredentialsResponse{Ok: true}, nil
}

// Login получает user_id от gateway (тот спрашивает user-service по username/email).
func (s *Auth) Login(ctx context.Context, req *authpb.LoginRequest) (*authpb.LoginResponse, error) {
	var hash string
	err := s.db.QueryRowContext(ctx,
		`SELECT password_hash FROM credentials WHERE user_id = $1`,
		req.UserId,
	).Scan(&hash)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid credentials")
	}

	token, err := jwt.Issue(req.UserId)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to issue token")
	}

	return &authpb.LoginResponse{Token: token}, nil
}

// ValidateToken вызывается gateway на каждый входящий запрос.
// Результат кешируется в Redis чтобы не парсить JWT каждый раз.
func (s *Auth) ValidateToken(ctx context.Context, req *authpb.ValidateTokenRequest) (*authpb.ValidateTokenResponse, error) {
	key := tokenCachePrefix + req.Token

	if userID, err := s.cache.Get(ctx, key).Result(); err == nil {
		return &authpb.ValidateTokenResponse{Valid: true, UserId: userID}, nil
	}

	userID, err := jwt.Parse(req.Token)
	if err != nil {
		return &authpb.ValidateTokenResponse{Valid: false}, nil
	}

	s.cache.Set(ctx, key, userID, tokenCacheTTL)
	return &authpb.ValidateTokenResponse{Valid: true, UserId: userID}, nil
}
