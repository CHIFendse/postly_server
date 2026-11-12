package router

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"gateway-service/internal/clients"
	"gateway-service/internal/router/middlewares"
	authpb "postly/proto/auth"
	s3pb "postly/proto/s3"
	userpb "postly/proto/user"
)

var registerCfg *middlewares.AttemptsConfig = &middlewares.AttemptsConfig{
	Name: "register",
	MaxAttempts: 5, FailWindow: 15 * time.Minute, BaseCooldown: 15 * time.Minute,
	MaxCooldown: 15 * time.Minute, StrikeTTL: 24 * time.Hour, Refund: false,
}

func RegisterUser(mux *http.ServeMux, c *clients.Clients, cache *redis.Client) {
	mux.Handle("/register", middlewares.Chain(handleRegister(c), middlewares.AttemptsMiddleware(cache, registerCfg)))
	mux.Handle("/getAvatarUploadUrl", middlewares.Chain(handleGetAvatarUploadURL(c), middlewares.JWTMiddleware(c)))
	mux.Handle("/setAvatar", middlewares.Chain(handleSetAvatar(c), middlewares.JWTMiddleware(c)))
	mux.Handle("/deleteAvatar", middlewares.Chain(handleDeleteAvatar(c), middlewares.JWTMiddleware(c)))
	mux.Handle("/getAvatars", middlewares.Chain(handleGetAvatars(c), middlewares.JWTMiddleware(c)))
	mux.Handle("/avatar", middlewares.Chain(HandleAvatar(c), middlewares.JWTMiddleware(c)))
}

func avatarURLForKey(key string) string {
	if key == "" {
		return ""
	}
	return "/avatar?key=" + url.QueryEscape(key)
}

func isAvatarKey(key string) bool {
	parts := strings.Split(key, "/")
	return len(parts) == 3 && parts[0] == "avatars" && parts[1] != "" && parts[2] != ""
}

func ownAvatarKey(key, userID string) bool {
	return isAvatarKey(key) && strings.Split(key, "/")[1] == userID
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeMessage(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"message": msg})
}

// handleGetAvatarUploadURL godoc
//	@Summary		Get avatar upload URL
//	@Description	Generates a pre-signed S3 URL for uploading an avatar image (JPEG, PNG, WebP)
//	@Tags			user
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		swagger.GetAvatarUploadURLRequest	true	"File name and MIME type"
//	@Success		200		{object}	swagger.GetAvatarUploadURLResponse	"Pre-signed upload URL and S3 key"
//	@Failure		400		{object}	map[string]string					"Only JPEG, PNG and WebP are supported"
//	@Failure		401		"Unauthorized"
//	@Failure		405		{string}	string								"Method not allowed"
//	@Failure		500		{object}	map[string]string					"Failed to get upload URL"
//	@Router			/getAvatarUploadUrl [post]
func handleGetAvatarUploadURL(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.Context().Value(middlewares.UserIDKey).(string)

		var data struct {
			FileName    string `json:"file_name"`
			ContentType string `json:"content_type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		resp, err := c.S3.GetUploadUrl(r.Context(), &s3pb.GetUploadUrlRequest{
			UserId:      userID,
			FileName:    data.FileName,
			ContentType: data.ContentType,
			Folder:      "avatars",
		})
		if status.Code(err) == codes.InvalidArgument {
			writeMessage(w, http.StatusBadRequest, "Only JPEG, PNG and WebP are supported")
			return
		}
		if err != nil {
			log.Printf("[GW] getAvatarUploadUrl user=%s: %v", userID, err)
			writeMessage(w, http.StatusInternalServerError, "Failed to get upload URL")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"upload_url": resp.UploadUrl,
			"s3_key":     resp.S3Key,
		})
	}
}

// handleSetAvatar godoc
//	@Summary		Set active avatar
//	@Description	Assigns an uploaded S3 avatar key as the active profile avatar
//	@Tags			user
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		swagger.SetAvatarRequest	true	"S3 key of the uploaded avatar"
//	@Success		200		{object}	swagger.StatusResponse		"Avatar updated successfully"
//	@Failure		400		{object}	map[string]string			"Invalid file key"
//	@Failure		401		"Unauthorized"
//	@Failure		403		{object}	map[string]string			"Forbidden file key"
//	@Failure		405		{string}	string						"Method not allowed"
//	@Failure		409		{object}	map[string]string			"This avatar is already set"
//	@Failure		500		{object}	map[string]string			"Failed to set avatar"
//	@Router			/setAvatar [post]
func handleSetAvatar(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.Context().Value(middlewares.UserIDKey).(string)

		var data struct {
			S3Key string `json:"s3_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		if !ownAvatarKey(data.S3Key, userID) {
			writeMessage(w, http.StatusForbidden, "Forbidden file key")
			return
		}

		_, err := c.User.SetAvatar(r.Context(), &userpb.SetAvatarRequest{UserId: userID, S3Key: data.S3Key})
		switch status.Code(err) {
		case codes.OK:
			writeJSON(w, http.StatusOK, map[string]bool{"status": true})
		case codes.AlreadyExists:
			writeMessage(w, http.StatusConflict, "This avatar is already set")
			return
		case codes.InvalidArgument:
			writeMessage(w, http.StatusBadRequest, "Invalid file key")
			return
		default:
			log.Printf("[GW] setAvatar user=%s: %v", userID, err)
			writeMessage(w, http.StatusInternalServerError, "Failed to set avatar")
			return
		}
	}
}

// handleDeleteAvatar godoc
//	@Summary		Delete avatar
//	@Description	Removes an avatar from the user profile and deletes the file from S3 storage
//	@Tags			user
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		swagger.DeleteAvatarRequest	true	"S3 key of the avatar to delete"
//	@Success		200		{object}	swagger.StatusResponse		"Avatar deleted successfully"
//	@Failure		400		{string}	string						"Bad request"
//	@Failure		401		"Unauthorized"
//	@Failure		404		{object}	map[string]string			"Avatar not found"
//	@Failure		405		{string}	string						"Method not allowed"
//	@Failure		500		{object}	map[string]string			"Failed to delete avatar"
//	@Router			/deleteAvatar [post]
func handleDeleteAvatar(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.Context().Value(middlewares.UserIDKey).(string)

		var data struct {
			S3Key string `json:"s3_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil || data.S3Key == "" {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		_, err := c.User.DeleteAvatar(r.Context(), &userpb.DeleteAvatarRequest{UserId: userID, S3Key: data.S3Key})
		switch status.Code(err) {
		case codes.OK:
		case codes.NotFound:
			writeMessage(w, http.StatusNotFound, "Avatar not found")
			return
		default:
			log.Printf("[GW] deleteAvatar user=%s: %v", userID, err)
			writeMessage(w, http.StatusInternalServerError, "Failed to delete avatar")
			return
		}

		if _, err := c.S3.DeleteFile(r.Context(), &s3pb.DeleteFileRequest{S3Key: data.S3Key}); err != nil {
			log.Printf("[GW] deleteAvatar S3 key=%s: %v", data.S3Key, err)
		}

		writeJSON(w, http.StatusOK, map[string]bool{"status": true})
	}
}

// handleGetAvatars godoc
//	@Summary		Get avatar history
//	@Description	Returns a list of all avatars uploaded by a user. Defaults to the authorized user if user_id query parameter is not provided.
//	@Tags			user
//	@Produce		json
//	@Security		BearerAuth
//	@Param			user_id	query		string				false	"Optional target user ID"	example("usr_12345")
//	@Success		200		{array}		swagger.AvatarItem	"List of avatars"
//	@Failure		401		"Unauthorized"
//	@Failure		405		{string}	string				"Method not allowed"
//	@Failure		500		{object}	map[string]string	"Failed to get avatars"
//	@Router			/getAvatars [get]
func handleGetAvatars(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.URL.Query().Get("user_id")
		if userID == "" {
			userID = r.Context().Value(middlewares.UserIDKey).(string)
		}

		resp, err := c.User.GetAvatars(r.Context(), &userpb.GetAvatarsRequest{UserId: userID})
		if err != nil {
			log.Printf("[GW] getAvatars user=%s: %v", userID, err)
			writeMessage(w, http.StatusInternalServerError, "Failed to get avatars")
			return
		}

		type avatarOut struct {
			URL       string `json:"url"`
			S3Key     string `json:"s3_key"`
			CreatedAt int64  `json:"created_at"`
		}
		out := make([]avatarOut, 0, len(resp.Avatars))
		for _, a := range resp.Avatars {
			out = append(out, avatarOut{
				URL:       avatarURLForKey(a.S3Key),
				S3Key:     a.S3Key,
				CreatedAt: a.CreatedAt * 1000,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// HandleAvatar godoc
//	@Summary		Get avatar image
//	@Description	Streams the binary image of an avatar from S3 by key or user_id
//	@Tags			user
//	@Produce		image/*
//	@Produce		octet-stream
//	@Security		BearerAuth
//	@Param			key		query		string	false	"Avatar S3 key"		example("avatars/usr_12345/avatar.png")
//	@Param			user_id	query		string	false	"Target user ID"	example("usr_12345")
//	@Success		200		{file}		binary	"Avatar binary stream"
//	@Failure		400		{string}	string	"key or user_id required"
//	@Failure		401		{string}	string	"Unauthorized"
//	@Failure		404		{string}	string	"Not found"
//	@Failure		405		{string}	string	"Method not allowed"
//	@Failure		500		{string}	string	"Internal error"
//	@Router			/avatar [get]
func HandleAvatar(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if _, ok := authenticate(c, r); !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		key := r.URL.Query().Get("key")
		cacheControl := "private, max-age=86400"
		if key == "" {
			userID := r.URL.Query().Get("user_id")
			if userID == "" {
				http.Error(w, "key or user_id required", http.StatusBadRequest)
				return
			}
			info, err := c.User.GetUserInfo(r.Context(), &userpb.GetUserInfoRequest{UserId: userID})
			if status.Code(err) == codes.NotFound {
				http.Error(w, "Not found", http.StatusNotFound)
				return
			}
			if err != nil {
				log.Printf("[GW] avatar: GetUserInfo user=%s: %v", userID, err)
				http.Error(w, "Internal error", http.StatusInternalServerError)
				return
			}
			key = info.S3Key
		}

		if !isAvatarKey(key) {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		proxyS3(w, r, c, key, "inline", cacheControl)
	}
}

// handleRegister godoc
//	@Summary		User registration
//	@Description	Registers a new user and sets initial authentication credentials. Rate limited by IP/username.
//	@Tags			user
//	@Accept			json
//	@Produce		json
//	@Param			request	body		swagger.RegisterRequest	true	"Registration details"
//	@Success		200		{object}	swagger.StatusResponse	"Registered successfully"
//	@Failure		400		{string}	string					"Bad request"
//	@Failure		405		{string}	string					"Method not allowed"
//	@Failure		409		{object}	map[string]string		"Username or email already exists"
//	@Failure		500		{object}	map[string]string		"Registration failed. Try again."
//	@Router			/register [post]
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
			msg := "Registration error"
			if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "23505") {
				if strings.Contains(err.Error(), "username") {
					msg = "Username already exists"
				} else if strings.Contains(err.Error(), "email") {
					msg = "Email already exists"
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
			if _, delErr := c.User.DeleteUser(r.Context(), &userpb.DeleteUserRequest{UserId: userResp.UserId}); delErr != nil {
				log.Printf("cleanup DeleteUser error: %v", delErr)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"message": "Registration failed. Try again."})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"status": true})
	}
}