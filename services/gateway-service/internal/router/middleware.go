package router

import (
	"context"
	"net/http"
	"strings"

	"gateway-service/internal/clients"
	authpb "postly/proto/auth"
)
type Middleware func(http.HandlerFunc) http.HandlerFunc
type contextKey string
const UserIDKey contextKey = "user_id"


func Chain(next http.HandlerFunc, middlewares ...Middleware) http.HandlerFunc {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](next)
		}
		return func(w http.ResponseWriter, r *http.Request) {
			next(w, r)
		}
}

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

func JWTMiddleware(c *clients.Clients) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc{
		return func(w http.ResponseWriter, r *http.Request) {
			userID, ok := authenticate(c, r)
			if !ok {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			next(w, r.WithContext(ctx))
		}
	}
}


func AttemptsMiddleware(c *clients.Clients) func (http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			
			next(w, r)
		}
	}
}