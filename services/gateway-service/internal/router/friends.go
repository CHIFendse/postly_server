package router

import (
	"encoding/json"
	"net/http"

	"gateway-service/internal/clients"
	friendspb "postly/proto/friends"
	userpb "postly/proto/user"
)

func RegisterFriends(mux *http.ServeMux, c *clients.Clients) {
	mux.HandleFunc("/sendFriendRequest",    JWTMiddleware(c, handleSendFriendRequest(c)))
	mux.HandleFunc("/getFriendRequests",    JWTMiddleware(c, handleGetFriendRequests(c)))
	mux.HandleFunc("/acceptFriendRequest",  JWTMiddleware(c, handleAcceptFriendRequest(c)))
	mux.HandleFunc("/declineFriendRequest", JWTMiddleware(c, handleDeclineFriendRequest(c)))
	mux.HandleFunc("/getFriends",           JWTMiddleware(c, handleGetFriends(c)))
}

func handleSendFriendRequest(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.Context().Value(UserIDKey).(string)

		var data struct {
			Username string `json:"username"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		target, err := c.User.GetUserByUsername(r.Context(), &userpb.GetUserByUsernameRequest{Username: data.Username})
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"message": "Пользователь не найден"})
			return
		}

		resp, err := c.Friends.SendFriendRequest(r.Context(), &friendspb.SendFriendRequestReq{
			SenderId:   userID,
			ReceiverId: target.UserId,
		})
		if err != nil {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"request_id": resp.RequestId})
	}
}

func handleGetFriendRequests(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(UserIDKey).(string)

		resp, err := c.Friends.GetFriendRequests(r.Context(), &friendspb.GetFriendRequestsReq{UserId: userID})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"message": "Ошибка получения заявок"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp.Requests)
	}
}

func handleAcceptFriendRequest(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.Context().Value(UserIDKey).(string)

		var data struct {
			RequestID string `json:"request_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		_, err := c.Friends.AcceptFriendRequest(r.Context(), &friendspb.AcceptFriendRequestReq{
			UserId:    userID,
			RequestId: data.RequestID,
		})
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"message": "Заявка не найдена"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"status": true})
	}
}

func handleDeclineFriendRequest(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.Context().Value(UserIDKey).(string)

		var data struct {
			RequestID string `json:"request_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		_, err := c.Friends.DeclineFriendRequest(r.Context(), &friendspb.DeclineFriendReq{
			UserId:    userID,
			RequestId: data.RequestID,
		})
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"message": "Заявка не найдена"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"status": true})
	}
}

func handleGetFriends(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(UserIDKey).(string)

		resp, err := c.Friends.GetFriends(r.Context(), &friendspb.GetFriendsReq{UserId: userID})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"message": "Ошибка получения друзей"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp.Friends)
	}
}
