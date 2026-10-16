package middlewares

import (
	"gateway-service/internal/clients"
	"net/http"
	"strings"
	"context"

	authpb "postly/proto/auth"
)


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


func JWTMiddleware(c *clients.Clients) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := authenticate(c, r)
			if !ok {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
