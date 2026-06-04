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
	mux.HandleFunc("/getFriends",            JWTMiddleware(c, handleGetFriends(c)))
	mux.HandleFunc("/deleteFriend",          JWTMiddleware(c, handleDeleteFriend(c)))
	mux.HandleFunc("/cancelFriendRequest",   JWTMiddleware(c, handleCancelFriendRequest(c)))
	mux.HandleFunc("/getSentRequests",       JWTMiddleware(c, handleGetSentRequests(c)))
}

func handleDeleteFriend(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.Context().Value(UserIDKey).(string)
		var data struct {
			FriendId string `json:"friend_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		_, err := c.Friends.DeleteFriend(r.Context(), &friendspb.DeleteFriendReq{
			UserId1: userID,
			UserId2: data.FriendId,
		})
		if err != nil {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"success":"error", "message": err.Error()})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"success": "ok"})

	}
}

func handleSendFriendRequest(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.Context().Value(UserIDKey).(string)

		var data struct {
			Username       string `json:"username"`
			TargetUsername string `json:"target_username"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		if data.TargetUsername != "" {
			data.Username = data.TargetUsername
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

		type reqOut struct {
			Id        string `json:"id"`
			SenderId  string `json:"sender_id"`
			Username  string `json:"username"`
			CreatedAt int64  `json:"created_at"`
		}
		out := make([]reqOut, 0, len(resp.Requests))
		for _, req := range resp.Requests {
			username := req.SenderId
			u, err := c.User.GetUserByUserId(r.Context(), &userpb.GetUserByUserIdRequest{UserId: req.SenderId})
			if err == nil {
				username = u.Username
			}
			out = append(out, reqOut{
				Id:        req.Id,
				SenderId:  req.SenderId,
				Username:  username,
				CreatedAt: req.CreatedAt,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
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

		type friendOut struct {
			Id       string `json:"id"`
			Username string `json:"username"`
		}
		out := make([]friendOut, 0, len(resp.Friends))
		for _, f := range resp.Friends {
			username := f.UserId
			if u, err := c.User.GetUserByUserId(r.Context(), &userpb.GetUserByUserIdRequest{UserId: f.UserId}); err == nil {
				username = u.Username
			}
			out = append(out, friendOut{Id: f.UserId, Username: username})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}
}

func handleCancelFriendRequest(c *clients.Clients) http.HandlerFunc {
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

		_, err := c.Friends.CancelFriendRequest(r.Context(), &friendspb.DeclineFriendReq{
			UserId:    userID,
			RequestId: data.RequestID,
		})
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"message": "Заявка не найдена"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
}

func handleGetSentRequests(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(UserIDKey).(string)

		resp, err := c.Friends.GetSentRequests(r.Context(), &friendspb.GetFriendRequestsReq{UserId: userID})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"message": "Ошибка получения заявок"})
			return
		}

		type reqOut struct {
			Id         string `json:"id"`
			ReceiverId string `json:"receiver_id"`
			Username   string `json:"username"`
			CreatedAt  int64  `json:"created_at"`
		}
		out := make([]reqOut, 0, len(resp.Requests))
		for _, req := range resp.Requests {
			// SenderId содержит receiver_id (см. GetSentRequests в friends-service)
			username := req.SenderId
			if u, err := c.User.GetUserByUserId(r.Context(), &userpb.GetUserByUserIdRequest{UserId: req.SenderId}); err == nil {
				username = u.Username
			}
			out = append(out, reqOut{
				Id:         req.Id,
				ReceiverId: req.SenderId,
				Username:   username,
				CreatedAt:  req.CreatedAt,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}
}
