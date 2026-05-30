package router

import (
	"encoding/json"
	"gateway-service/internal/clients"
	"net/http"
	authpb "postly/proto/auth"
	userpb "postly/proto/user"
)

func RegisterAuth(mux *http.ServeMux, c *clients.Clients) {
	mux.HandleFunc("/login", handleLogin(c))
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

		tokenResp, err := c.Auth.Login(r.Context(), &authpb.LoginRequest{UserId: userResp.UserId, Password: data.Password})
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"message": "Неверный логин или пароль"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"token": tokenResp.Token})
	}
}

