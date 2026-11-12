package router

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"gateway-service/internal/clients"
	"gateway-service/internal/router/initrouter"
	"gateway-service/internal/router/middlewares"
	chatpb "postly/proto/chat"
	userpb "postly/proto/user"

	"github.com/redis/go-redis/v9"
)

func RegisterChat(mux *http.ServeMux, c *clients.Clients, cache *redis.Client) {
	mux.Handle("/getChats",    middlewares.Chain(HandleGetChats(c), middlewares.JWTMiddleware(c)))
	mux.Handle("/getGroups",   middlewares.Chain(HandleGetGroups(c), middlewares.JWTMiddleware(c)))
	mux.Handle("/createChat",  middlewares.Chain(handleCreateChat(c, cache), middlewares.JWTMiddleware(c)))
	mux.Handle("/createGroup", middlewares.Chain(handleCreateGroup(c, cache), middlewares.JWTMiddleware(c)))
	mux.Handle("/clearChat",   middlewares.Chain(handleClearChat(c, cache), middlewares.JWTMiddleware(c)))
	mux.Handle("/deleteChat",  middlewares.Chain(handleDeleteChat(c, cache), middlewares.JWTMiddleware(c)))
	mux.Handle("/deleteGroup", middlewares.Chain(handleDeleteGroup(c, cache), middlewares.JWTMiddleware(c)))
}

// HandleGetChats godoc
//	@Summary		Getting chats
//	@Description	Returning the list of chats by JWT Token
//	@Tags			chat
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{array}		initrouter.Chats	"The list of chats"
//	@Failure		401	"Unauthorized"
//	@Failure		500	{string}	string	"Error getting chats"
//	@Router			/getChats [get]
func HandleGetChats(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(middlewares.UserIDKey).(string)

		out, err := initrouter.GetChats(r.Context(), c, userID)
		if err != nil {
			http.Error(w, "Error getting chats", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}
}

// HandleGetGroups godoc
//	@Summary		Getting groups
//	@Description	Getting user groups by JWT Token
//	@Tags			chat
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{array}		initrouter.Groups	"The list of groups"
//	@Failure		401	"Unauthorized"
//	@Failure		500	{string}	string	"Error getting groups"
//	@Router			/getGroups [get]
func HandleGetGroups(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(middlewares.UserIDKey).(string)

		out, err := initrouter.GetGroups(r.Context(), c, userID)
		if err != nil {
			http.Error(w, "Error getting groups", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}
}

// handleCreateChat godoc
//	@Summary		Creating chat
//	@Description	Create a new chat if it doesn't exist; otherwise, get its ID
//	@Tags			chat
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		swagger.CreateChatRequest	true	"username and target_username in the body"
//	@Success		200		{object}	swagger.CreateChatResponse	"New chat"
//	@Failure		400		{string}	string						"Bad request"
//	@Failure		401		"Unauthorized"
//	@Failure		404		{object}	map[string]string			"User was not found"
//	@Failure		405		{string}	string						"Method not allowed"
//	@Failure		500		{object}	map[string]string			"Error creating chat"
//	@Router			/createChat [post]
func handleCreateChat(c *clients.Clients, cache *redis.Client) http.HandlerFunc {
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

		targetResp, err := c.User.GetUserByUsername(r.Context(), &userpb.GetUserByUsernameRequest{Username: data.Username})
		if err != nil {
			log.Printf("createChat: GetUserByUsername(%q) err: %v", data.Username, err)
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"message": "User was not found"})
			return
		}

		resp, err := c.Chat.CreateChat(r.Context(), &chatpb.CreateChatRequest{
			UserId:         userID,
			TargetUsername: targetResp.UserId,
		})
		if err != nil {
			log.Printf("createChat: Chat.CreateChat err: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"message": "Error creating chat"})
			return
		}

		go func() {
			defer middlewares.RecoveryMonitor()
			payload, _ := json.Marshal(map[string]string{
				"type":    "NEW_CHAT",
				"chat_id": resp.ChatId,
			})
			cache.Publish(context.Background(), "ws:user:"+targetResp.UserId, payload)
		}()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// handleCreateGroup godoc
//	@Summary		Creating group
//	@Description	Create a new group if it doesn't exist; otherwise, get its ID
//	@Tags			chat
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		swagger.CreateGroupRequest	true	"name, members, avatar in the body"
//	@Success		200		{object}	swagger.CreateGroupResponse	"New group details"
//	@Failure		400		{string}	string						"Bad request"
//	@Failure		401		"Unauthorized"
//	@Failure		405		{string}	string						"Method not allowed"
//	@Failure		500		{object}	map[string]string			"Error creating group"
//	@Router			/createGroup [post]
func handleCreateGroup(c *clients.Clients, cache *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.Context().Value(middlewares.UserIDKey).(string)

		var data struct {
			Name        string   `json:"name"`
			IsPrivate   bool     `json:"is_private"`
			Members     []string `json:"members"`
			AvatarColor string   `json:"avatar_color"`
			AvatarFile  string   `json:"avatar_file"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		memberIDs := []string{}
		notFound := []string{}
		for _, username := range data.Members {
			u, err := c.User.GetUserByUsername(r.Context(), &userpb.GetUserByUsernameRequest{Username: username})
			if err != nil {
				notFound = append(notFound, username)
				continue
			}
			memberIDs = append(memberIDs, u.UserId)
		}

		resp, err := c.Chat.CreateGroup(r.Context(), &chatpb.CreateGroupRequest{
			Name:        data.Name,
			AdminId:     userID,
			IsPrivate:   data.IsPrivate,
			Members:     memberIDs,
			AvatarColor: data.AvatarColor,
			AvatarFile:  data.AvatarFile,
		})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"message": "Error creating group"})
			return
		}

		go func(groupID string, ids []string) {
			defer middlewares.RecoveryMonitor()
			payload, _ := json.Marshal(map[string]string{
				"type":    "NEW_CHAT",
				"chat_id": groupID,
			})
			for _, memberID := range ids {
				cache.Publish(context.Background(), "ws:user:"+memberID, payload)
			}
		}(resp.GroupId, memberIDs)

		w.Header().Set("Content-Type", "application/json")
		type groupCreateOut struct {
			GroupId  string   `json:"group_id"`
			Id       string   `json:"id"`
			NotFound []string `json:"not_found,omitempty"`
		}
		json.NewEncoder(w).Encode(groupCreateOut{
			GroupId:  resp.GroupId,
			Id:       resp.GroupId,
			NotFound: notFound,
		})
	}
}

// handleClearChat godoc
//	@Summary		Clearing the chat
//	@Description	Clears message history for a specific chat by ID
//	@Tags			chat
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		swagger.ClearChatRequest	true	"chat_id in the body"
//	@Success		200		{object}	map[string]bool				"Status: true"
//	@Failure		400		{string}	string						"Bad request"
//	@Failure		401		"Unauthorized"
//	@Failure		403		{object}	map[string]string			"Error clearing chat (forbidden)"
//	@Failure		405		{string}	string						"Method not allowed"
//	@Router			/clearChat [post]
func handleClearChat(c *clients.Clients, cache *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.Context().Value(middlewares.UserIDKey).(string)

		var data struct {
			ChatId string `json:"chat_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		pts, err := c.Chat.GetParticipants(r.Context(), &chatpb.GetParticipantsRequest{ChatId: data.ChatId})
		if err != nil {
			log.Printf("GetParticipants error: %v", err)
		}
		_, err = c.Chat.ClearChat(r.Context(), &chatpb.ClearChatRequest{
			ChatId: data.ChatId,
			UserId: userID,
		})
		if err != nil {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"message": "Error clearing chat"})
			return
		}

		if pts != nil {
			go func() {
				defer middlewares.RecoveryMonitor()
				payload, _ := json.Marshal(map[string]string{
					"type":    "CLEAR_CHAT",
					"chat_id": data.ChatId,
				})
				for _, uid := range pts.UserIds {
					cache.Publish(context.Background(), "ws:user:"+uid, payload)
				}
			}()
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"status": true})
	}
}

// handleDeleteChat godoc
//	@Summary		Deleting chat
//	@Description	Completely deletes a private chat by chat_id
//	@Tags			chat
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		swagger.DeleteChatRequest	true	"chat_id in the body"
//	@Success		200		{object}	map[string]bool				"Status: true"
//	@Failure		400		{string}	string						"Bad request"
//	@Failure		401		"Unauthorized"
//	@Failure		403		{object}	map[string]string			"Error deleting chat"
//	@Failure		405		{string}	string						"Method not allowed"
//	@Router			/deleteChat [post]
func handleDeleteChat(c *clients.Clients, cache *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.Context().Value(middlewares.UserIDKey).(string)

		var data struct {
			ChatId string `json:"chat_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		pts, _ := c.Chat.GetParticipants(r.Context(), &chatpb.GetParticipantsRequest{ChatId: data.ChatId})

		_, err := c.Chat.DeleteChat(r.Context(), &chatpb.DeleteChatRequest{
			ChatId: data.ChatId,
			UserId: userID,
		})
		if err != nil {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"message": "Ошибка удаления чата"})
			return
		}

		if pts != nil {
			go func() {
				defer middlewares.RecoveryMonitor()
				payload, _ := json.Marshal(map[string]string{
					"type":    "DELETE_CHAT",
					"chat_id": data.ChatId,
				})
				for _, uid := range pts.UserIds {
					cache.Publish(context.Background(), "ws:user:"+uid, payload)
				}
			}()
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"status": true})
	}
}

// handleDeleteGroup godoc
//	@Summary		Deleting group
//	@Description	Completely deletes a group chat by chat_id
//	@Tags			chat
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		swagger.DeleteGroupRequest	true	"chat_id in the body"
//	@Success		200		{object}	map[string]bool				"Status: true"
//	@Failure		400		{string}	string						"Bad request"
//	@Failure		401		"Unauthorized"
//	@Failure		403		{object}	map[string]string			"Error deleting group"
//	@Failure		405		{string}	string						"Method not allowed"
//	@Router			/deleteGroup [post]
func handleDeleteGroup(c *clients.Clients, cache *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.Context().Value(middlewares.UserIDKey).(string)

		var data struct {
			ChatId string `json:"chat_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		pts, _ := c.Chat.GetParticipants(r.Context(), &chatpb.GetParticipantsRequest{ChatId: data.ChatId})

		_, err := c.Chat.DeleteGroup(r.Context(), &chatpb.DeleteGroupRequest{
			ChatId: data.ChatId,
			UserId: userID,
		})
		if err != nil {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"message": "Ошибка удаления чата"})
			return
		}

		if pts != nil {
			defer middlewares.RecoveryMonitor()
			go func() {
				payload, _ := json.Marshal(map[string]string{
					"type":    "DELETE_CHAT",
					"chat_id": data.ChatId,
				})
				for _, uid := range pts.UserIds {
					cache.Publish(context.Background(), "ws:user:"+uid, payload)
				}
			}()
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"status": true})
	}
}