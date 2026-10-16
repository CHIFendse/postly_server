package router

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"gateway-service/internal/clients"
	"gateway-service/internal/router/middlewares"
	authpb "postly/proto/auth"
	userpb "postly/proto/user"

	"github.com/redis/go-redis/v9"
)

var loginCfg *middlewares.AttemptsConfig = &middlewares.AttemptsConfig{Name: "login",
				MaxAttempts: 5, FailWindow: 15*time.Minute, BaseCooldown: 2*time.Minute,
				MaxCooldown: 2*time.Hour, StrikeTTL: 24*time.Hour, Refund: true,
			}

func RegisterAuth(mux *http.ServeMux, c *clients.Clients, cache *redis.Client) {
	mux.Handle("/login", middlewares.Chain(HandleLogin(c), middlewares.AttemptsMiddleware(cache, loginCfg)))
	mux.HandleFunc("/verify", HandleVerify(c))
}

func HandleLogin(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var data struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		userResp, err := c.User.GetUserByUsername(r.Context(), &userpb.GetUserByUsernameRequest{Username: data.Username})
		if err != nil {
			log.Printf("GetUserByUsername(%q) error: %v", data.Username, err)
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"message": "Неверный логин или пароль"})
			return
		}

		tokenResp, err := c.Auth.Login(r.Context(), &authpb.LoginRequest{UserId: userResp.UserId, Password: data.Password})
		if err != nil {
			log.Printf("login error: %v", err)
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"message": "Неверный логин или пароль"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"token":    tokenResp.Token,
			"id":       userResp.UserId,
			"username": data.Username,
		})
	}
}

func HandleVerify(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		resp, err := c.Auth.ValidateToken(r.Context(), &authpb.ValidateTokenRequest{Token: token})
		if err != nil || !resp.Valid {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"user_id": resp.UserId})
	}
}
