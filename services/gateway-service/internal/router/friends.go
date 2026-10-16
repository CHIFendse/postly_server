package router

import (
	"encoding/json"
	"fmt"
	"gateway-service/internal/clients"
	initrouter "gateway-service/internal/router/initrouter"
	"gateway-service/internal/router/middlewares"
	"net/http"
	"net/url"
	friendspb "postly/proto/friends"
	userpb "postly/proto/user"
)

func avatarURLForUser(userID string) string {
	if userID == "" {
		return ""
	}
	return "/avatar?user_id=" + url.QueryEscape(userID)
}


func RegisterFriends(mux *http.ServeMux, c *clients.Clients) {
	mux.Handle("/sendFriendRequest", 	middlewares.Chain(handleSendFriendRequest(c), middlewares.JWTMiddleware(c)))
	mux.Handle("/getFriendRequests", 	middlewares.Chain(HandleGetFriendRequests(c), middlewares.JWTMiddleware(c)))
	mux.Handle("/acceptFriendRequest",  middlewares.Chain(handleAcceptFriendRequest(c), middlewares.JWTMiddleware(c)))
	mux.Handle("/declineFriendRequest", middlewares.Chain(handleDeclineFriendRequest(c), middlewares.JWTMiddleware(c)))
	mux.Handle("/getFriends", 			middlewares.Chain(HandleGetFriends(c), middlewares.JWTMiddleware(c)))
	mux.Handle("/deleteFriend", 		middlewares.Chain(handleDeleteFriend(c), middlewares.JWTMiddleware(c)))
	mux.Handle("/cancelFriendRequest",  middlewares.Chain(handleCancelFriendRequest(c), middlewares.JWTMiddleware(c)))
	mux.Handle("/getSentRequests",		middlewares.Chain(HandleGetSentRequests(c), middlewares.JWTMiddleware(c)))
}

func handleDeleteFriend(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		user := r.Context().Value(middlewares.UserIDKey).(string)
		var data struct {
			FriendId string `json:"friend_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		_, err := c.Friends.DeleteFriend(r.Context(), &friendspb.DeleteFriendReq{
			UserId1: user,
			UserId2: data.FriendId,
		})
		fmt.Println(user)
		if err != nil {
			resp, err := c.User.GetUserByUsername(r.Context(), &userpb.GetUserByUsernameRequest{Username: data.FriendId})
			if err != nil {
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]string{"success": "error", "message": err.Error()})
				fmt.Printf("Error from GetUserByUsername, data: %s\n", data.FriendId)
				return
			}
			_, err = c.Friends.DeleteFriend(r.Context(), &friendspb.DeleteFriendReq{
				UserId1: user,
				UserId2: resp.UserId,
			})
			if err != nil {
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]string{"success": "error", "message": err.Error()})
				fmt.Printf("Error from DeleteFriend, data: %s\n", resp.UserId)
			}

		}

		json.NewEncoder(w).Encode(map[string]string{"success": "ok"})

	}
}

func handleSendFriendRequest(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		userID := r.Context().Value(middlewares.UserIDKey).(string)

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

func HandleGetFriendRequests(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := initrouter.GetFriendRequests(r.Context(), c, r.Context().Value(middlewares.UserIDKey).(string))
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
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
		userID := r.Context().Value(middlewares.UserIDKey).(string)

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
		userID := r.Context().Value(middlewares.UserIDKey).(string)

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

func HandleGetFriends(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := initrouter.GetFriends(r.Context(), c, r.Context().Value(middlewares.UserIDKey).(string))
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
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
		userID := r.Context().Value(middlewares.UserIDKey).(string)

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

func HandleGetSentRequests(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out, err := initrouter.GetSentRequests(r.Context(), c, r.Context().Value(middlewares.UserIDKey).(string))
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}
}
