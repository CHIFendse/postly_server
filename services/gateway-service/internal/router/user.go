package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"gateway-service/internal/clients"
	userpb "postly/proto/user"
	authpb "postly/proto/auth"
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

		 }
		
		_, err = c.Auth.SetCredentials(r.Context(), &authpb.SetCredentialsRequest{
			UserId: userResp.UserId, Password: data.Password,
		})
		if err != nil {

		}

		_ = fmt.Sprintf("%s %s", data.Username, strings.TrimSpace(data.Email)) // заглушка

		w.WriteHeader(http.StatusNotImplemented)
		json.NewEncoder(w).Encode(map[string]string{"message": "user-service not ready yet"})
	}
}
