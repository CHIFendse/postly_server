package router

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"gateway-service/internal/clients"
	authpb "postly/proto/auth"
	s3pb "postly/proto/s3"
	userpb "postly/proto/user"
)

func RegisterUser(mux *http.ServeMux, c *clients.Clients) {
	mux.HandleFunc("/register", handleRegister(c))
	mux.HandleFunc("/getAvatarUploadUrl", JWTMiddleware(c, handleGetAvatarUploadURL(c)))
	mux.HandleFunc("/setAvatar", JWTMiddleware(c, handleSetAvatar(c)))
	mux.HandleFunc("/deleteAvatar", JWTMiddleware(c, handleDeleteAvatar(c)))
	mux.HandleFunc("/getAvatars", JWTMiddleware(c, handleGetAvatars(c)))

	mux.HandleFunc("/avatar", handleAvatar(c))
}

func avatarURLFor(key string) string {
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

func handleGetAvatarUploadURL(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.Context().Value(UserIDKey).(string)

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
			writeMessage(w, http.StatusBadRequest, "Поддерживаются только JPEG, PNG и WebP")
			return
		}
		if err != nil {
			log.Printf("[GW] getAvatarUploadUrl user=%s: %v", userID, err)
			writeMessage(w, http.StatusInternalServerError, "Ошибка получения ссылки для загрузки")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"upload_url": resp.UploadUrl,
			"s3_key":     resp.S3Key,
		})
	}
}

func handleSetAvatar(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.Context().Value(UserIDKey).(string)

		var data struct {
			S3Key string `json:"s3_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		if !ownAvatarKey(data.S3Key, userID) {
			writeMessage(w, http.StatusForbidden, "Недопустимый ключ файла")
			return
		}

		_, err := c.User.SetAvatar(r.Context(), &userpb.SetAvatarRequest{UserId: userID, S3Key: data.S3Key})
		switch status.Code(err) {
		case codes.OK:
		case codes.AlreadyExists:
			writeMessage(w, http.StatusConflict, "Эта аватарка уже установлена")
			return
		case codes.InvalidArgument:
			writeMessage(w, http.StatusBadRequest, "Недопустимый ключ файла")
			return
		default:
			log.Printf("[GW] setAvatar user=%s: %v", userID, err)
			writeMessage(w, http.StatusInternalServerError, "Ошибка установки аватарки")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"avatar_url": avatarURLFor(data.S3Key)})
	}
}

func handleDeleteAvatar(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.Context().Value(UserIDKey).(string)

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
			writeMessage(w, http.StatusNotFound, "Аватарка не найдена")
			return
		default:
			log.Printf("[GW] deleteAvatar user=%s: %v", userID, err)
			writeMessage(w, http.StatusInternalServerError, "Ошибка удаления аватарки")
			return
		}

		if _, err := c.S3.DeleteFile(r.Context(), &s3pb.DeleteFileRequest{S3Key: data.S3Key}); err != nil {
			log.Printf("[GW] deleteAvatar S3 key=%s: %v", data.S3Key, err)
		}

		writeJSON(w, http.StatusOK, map[string]bool{"status": true})
	}
}

func handleGetAvatars(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID := r.URL.Query().Get("user_id")
		if userID == "" {
			userID = r.Context().Value(UserIDKey).(string)
		}

		resp, err := c.User.GetAvatars(r.Context(), &userpb.GetAvatarsRequest{UserId: userID})
		if err != nil {
			log.Printf("[GW] getAvatars user=%s: %v", userID, err)
			writeMessage(w, http.StatusInternalServerError, "Ошибка получения аватарок")
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
				URL:       avatarURLFor(a.S3Key),
				S3Key:     a.S3Key,
				CreatedAt: a.CreatedAt * 1000,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func handleAvatar(c *clients.Clients) http.HandlerFunc {
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

		proxyS3(w, r, c, key, "inline")
	}
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
			if _, delErr := c.User.DeleteUser(r.Context(), &userpb.DeleteUserRequest{UserId: userResp.UserId}); delErr != nil {
				log.Printf("cleanup DeleteUser error: %v", delErr)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"message": "Ошибка регистрации. Попробуйте снова."})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"status": true})
	}
}
