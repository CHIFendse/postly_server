package initrouter

import (
	"encoding/json"
	"fmt"
	"gateway-service/internal/clients"
	"gateway-service/internal/router/middlewares"
	"net/http"
)

type initResponse struct {
	Friends        []friendOut `json:"friends"`
	FriendRequests []reqOut    `json:"friend_requests"`
	SentRequests   []reqOut    `json:"sent_requests"`
	Chats          []Chats     `json:"chats"`
	Groups         []Groups    `json:"groups"`
}

func RegisterInitClient(mux *http.ServeMux, c *clients.Clients) {
	mux.Handle("/init_client", middlewares.Chain(handlerInit(c), middlewares.JWTMiddleware(c)))
}

func handlerInit(c *clients.Clients) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(middlewares.UserIDKey).(string)
		chats, err := GetChats(r.Context(), c, userID)
		if err != nil {
			http.Error(w, "Error of getting user chats", http.StatusInternalServerError)
			fmt.Printf("[ERROR] ID: %v, err: %s\n", r.Context().Value(middlewares.RequestIDKey), err)
			return
		}
		groups, err := GetGroups(r.Context(), c, userID)
		if err != nil {
			http.Error(w, "Error of getting user groups", http.StatusInternalServerError)
			fmt.Printf("[ERROR] ID: %v, err: %s\n", r.Context().Value(middlewares.RequestIDKey), err)
			return
		}
		friends, err := GetFriends(r.Context(), c, userID)
		if err != nil {
			http.Error(w, "Error of getting user friends", http.StatusInternalServerError)
			fmt.Printf("[ERROR] ID: %v, err: %s\n", r.Context().Value(middlewares.RequestIDKey), err)
			return
		}
		friendRequests, err := GetFriendRequests(r.Context(), c, userID)
		if err != nil {
			http.Error(w, "Error of getting friend requests", http.StatusInternalServerError)
			fmt.Printf("[ERROR] ID: %v, err: %s\n", r.Context().Value(middlewares.RequestIDKey), err)
			return
		}
		sentRequests, err := GetSentRequests(r.Context(), c, userID)
		if err != nil {
			http.Error(w, "Error of getting sent requests", http.StatusInternalServerError)
			fmt.Printf("[ERROR] ID: %v, err: %s\n", r.Context().Value(middlewares.RequestIDKey), err)
			return
		}
		data := initResponse{Friends: friends,
			FriendRequests: friendRequests, SentRequests: sentRequests,
			Chats: chats, Groups: groups}
		dataJson, err := json.Marshal(data)
		if err != nil {
			http.Error(w, "Error of json encoder", http.StatusInternalServerError)
			fmt.Printf("[ERROR] ID: %v, err: %s\n", r.Context().Value(middlewares.RequestIDKey), err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(dataJson)
	})
}
