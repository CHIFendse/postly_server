package router

import (
	"context"
	"net/http"
	"strings"

	"gateway-service/internal/clients"
	authpb "postly/proto/auth"
)

type contextKey string

const UserIDKey contextKey = "user_id"

func JWTMiddleware(c *clients.Clients, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		resp, err := c.Auth.ValidateToken(r.Context(), &authpb.ValidateTokenRequest{Token: token})
		if err != nil || !resp.Valid {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), UserIDKey, resp.UserId)
		next(w, r.WithContext(ctx))
	}
}
