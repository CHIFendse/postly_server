package router

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"gateway-service/internal/clients"
	authpb "postly/proto/auth"
	userpb "postly/proto/user"
)

func RegisterAuth(mux *http.ServeMux, c *clients.Clients) {
	mux.HandleFunc("/login",  handleLogin(c))
	mux.HandleFunc("/verify", handleVerify(c))
}

func handleLogin(c *clients.Clients) http.HandlerFunc {
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
			"token": tokenResp.Token,
			"id":    userResp.UserId,
		})
	}
}

// /verify — клиент проверяет токен при старте приложения
func handleVerify(c *clients.Clients) http.HandlerFunc {
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
