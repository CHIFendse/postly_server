package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"gateway-service/internal/clients"
	initrouter "gateway-service/internal/router/initrouter"
	"gateway-service/internal/router/middlewares"
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
	mux.Handle("/sendFriendRequest",    middlewares.Chain(handleSendFriendRequest(c), middlewares.JWTMiddleware(c)))
	mux.Handle("/getFriendRequests",    middlewares.Chain(HandleGetFriendRequests(c), middlewares.JWTMiddleware(c)))
	mux.Handle("/acceptFriendRequest",  middlewares.Chain(handleAcceptFriendRequest(c), middlewares.JWTMiddleware(c)))
	mux.Handle("/declineFriendRequest", middlewares.Chain(handleDeclineFriendRequest(c), middlewares.JWTMiddleware(c)))
	mux.Handle("/getFriends",           middlewares.Chain(HandleGetFriends(c), middlewares.JWTMiddleware(c)))
	mux.Handle("/deleteFriend",         middlewares.Chain(handleDeleteFriend(c), middlewares.JWTMiddleware(c)))
	mux.Handle("/cancelFriendRequest",  middlewares.Chain(handleCancelFriendRequest(c), middlewares.JWTMiddleware(c)))
	mux.Handle("/getSentRequests",      middlewares.Chain(HandleGetSentRequests(c), middlewares.JWTMiddleware(c)))
}

// handleDeleteFriend godoc
//	@Summary		Delete friend
//	@Description	Removes a user from friends by user ID or username
//	@Tags			friends
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		swagger.DeleteFriendRequest	true	"Friend ID or username"
//	@Success		200		{object}	swagger.SuccessResponse		"Friend deleted successfully"
//	@Failure		400		{string}	string						"Bad request"
//	@Failure		401		"Unauthorized"
//	@Failure		405		{string}	string						"Method not allowed"
//	@Failure		409		{object}	map[string]string			"Friend deletion conflict"
//	@Router			/deleteFriend [post]
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
				return
			}
		}

		json.NewEncoder(w).Encode(map[string]string{"success": "ok"})
	}
}

// handleSendFriendRequest godoc
//	@Summary		Send friend request
//	@Description	Sends a friendship invitation to target user by username
//	@Tags			friends
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		swagger.SendFriendRequestReq		true	"Recipient username"
//	@Success		200		{object}	swagger.SendFriendRequestResponse	"Request sent successfully"
//	@Failure		400		{string}	string								"Bad request"
//	@Failure		401		"Unauthorized"
//	@Failure		404		{object}	map[string]string					"User not found"
//	@Failure		405		{string}	string								"Method not allowed"
//	@Failure		409		{object}	map[string]string					"Friend request conflict"
//	@Router			/sendFriendRequest [post]
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
			json.NewEncoder(w).Encode(map[string]string{"message": "User not found"})
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

// HandleGetFriendRequests godoc
//	@Summary		Get received friend requests
//	@Description	Returns a list of all incoming friend requests for the authorized user
//	@Tags			friends
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{array}		swagger.FriendRequestItem	"List of received friend requests"
//	@Failure		401	"Unauthorized"
//	@Failure		500	{string}	string	"Internal server error"
//	@Router			/getFriendRequests [get]
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

// handleAcceptFriendRequest godoc
//	@Summary		Accept friend request
//	@Description	Accepts an incoming friend request by request ID
//	@Tags			friends
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		swagger.FriendActionRequest	true	"Request ID"
//	@Success		200		{object}	swagger.StatusResponse		"Status: true"
//	@Failure		400		{string}	string						"Bad request"
//	@Failure		401		"Unauthorized"
//	@Failure		404		{object}	map[string]string			"Request not found"
//	@Failure		405		{string}	string						"Method not allowed"
//	@Router			/acceptFriendRequest [post]
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
			json.NewEncoder(w).Encode(map[string]string{"message": "Request not found"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"status": true})
	}
}

// handleDeclineFriendRequest godoc
//	@Summary		Decline friend request
//	@Description	Declines an incoming friend request by request ID
//	@Tags			friends
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		swagger.FriendActionRequest	true	"Request ID"
//	@Success		200		{object}	swagger.StatusResponse		"Status: true"
//	@Failure		400		{string}	string						"Bad request"
//	@Failure		401		"Unauthorized"
//	@Failure		404		{object}	map[string]string			"Request not found"
//	@Failure		405		{string}	string						"Method not allowed"
//	@Router			/declineFriendRequest [post]
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
			json.NewEncoder(w).Encode(map[string]string{"message": "Request not found"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"status": true})
	}
}

// HandleGetFriends godoc
//	@Summary		Get friends list
//	@Description	Returns a list of all confirmed friends for the authorized user
//	@Tags			friends
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{array}		swagger.FriendItem	"List of friends"
//	@Failure		401	"Unauthorized"
//	@Failure		500	{string}	string	"Internal server error"
//	@Router			/getFriends [get]
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

// handleCancelFriendRequest godoc
//	@Summary		Cancel outgoing friend request
//	@Description	Cancels a pending outgoing friend request sent by the authorized user
//	@Tags			friends
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		swagger.FriendActionRequest	true	"Request ID"
//	@Success		200		{object}	swagger.OKResponse			"OK: true"
//	@Failure		400		{string}	string						"Bad request"
//	@Failure		401		"Unauthorized"
//	@Failure		404		{object}	map[string]string			"Request not found"
//	@Failure		405		{string}	string						"Method not allowed"
//	@Router			/cancelFriendRequest [post]
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
			json.NewEncoder(w).Encode(map[string]string{"message": "Request not found"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
}

// HandleGetSentRequests godoc
//	@Summary		Get sent friend requests
//	@Description	Returns a list of all pending outgoing friend requests sent by the authorized user
//	@Tags			friends
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{array}		swagger.SentRequestItem	"List of sent friend requests"
//	@Failure		401	"Unauthorized"
//	@Failure		500	{string}	string	"Internal server error"
//	@Router			/getSentRequests [get]
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