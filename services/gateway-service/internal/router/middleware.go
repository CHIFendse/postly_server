package router

import (
	"context"
	"net/http"
	"strings"
	"time"

	"gateway-service/internal/clients"
	authpb "postly/proto/auth"
)

type contextKey string

const UserIDKey contextKey = "user_id"

func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authenticate проверяет Bearer-токен и возвращает user_id
func authenticate(c *clients.Clients, r *http.Request) (string, bool) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		return "", false
	}

	resp, err := c.Auth.ValidateToken(r.Context(), &authpb.ValidateTokenRequest{Token: token})
	if err != nil || !resp.Valid {
		return "", false
	}
	return resp.UserId, true
}

func JWTMiddleware(c *clients.Clients, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := authenticate(c, r)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		ctx = context.WithValue(ctx, UserIDKey, userID)
		next(w, r.WithContext(ctx))
	}
}
