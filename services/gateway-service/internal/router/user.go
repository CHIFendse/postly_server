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

func RegisterUser(mux *http.ServeMux, c *clients.Clients) {
	mux.HandleFunc("/register", handleRegister(c))
}


func handleRegister(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var data struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Email    string `json:"email"`
			Phone    string `json:"phone"`
		}

		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		userResp, err := c.User.Register(r.Context(), &userpb.RegisterRequest{
			Username: data.Username, Email: data.Email, Phone: data.Phone,
		})
		if err != nil {
			log.Printf("user.Register error: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			msg := "Ошибка регистрации"
			if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "23505") {
				if strings.Contains(err.Error(), "username") {
					msg = "Пользователь с таким именем уже существует"
				} else if strings.Contains(err.Error(), "email") {
					msg = "Пользователь с такой почтой уже существует"
				}
			}
			json.NewEncoder(w).Encode(map[string]string{"message": msg})
			return
		}

		_, err = c.Auth.SetCredentials(r.Context(), &authpb.SetCredentialsRequest{
			UserId: userResp.UserId, Password: data.Password,
		})
		if err != nil {
			log.Printf("auth.SetCredentials error: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"message": "Ошибка сохранения пароля"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"status": true})
	}
}
