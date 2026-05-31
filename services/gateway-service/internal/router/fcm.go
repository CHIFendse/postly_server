package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/redis/go-redis/v9"
)

const fcmTokenPrefix = "fcm:user:"

func handleRegisterFCMToken(cache *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID, _ := r.Context().Value(UserIDKey).(string)

		var body struct {
			FCMToken string `json:"fcm_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.FCMToken == "" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		cache.Set(r.Context(), fcmTokenPrefix+userID, body.FCMToken, 0)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
}

// sendCallPush отправляет FCM push о входящем звонке.
// Требует FCM_SERVER_KEY в переменных окружения.
// Если ключ не задан — молча пропускает.
func sendCallPush(ctx context.Context, cache *redis.Client, recipientID, callerName, chatID string) {
	serverKey := os.Getenv("FCM_SERVER_KEY")
	if serverKey == "" {
		return
	}

	fcmToken, err := cache.Get(ctx, fcmTokenPrefix+recipientID).Result()
	if err != nil || fcmToken == "" {
		return
	}

	payload := map[string]any{
		"to":       fcmToken,
		"priority": "high",
		"notification": map[string]string{
			"title": "Входящий звонок",
			"body":  fmt.Sprintf("%s звонит вам", callerName),
			"sound": "default",
		},
		"data": map[string]string{
			"type":    "CALL_INVITE",
			"chat_id": chatID,
			"name":    callerName,
		},
		"android": map[string]any{
			"priority": "high",
			"notification": map[string]any{
				"channel_id":  "calls",
				"sound":       "default",
				"visibility":  "public",
				"importance":  "high",
			},
		},
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://fcm.googleapis.com/fcm/send", bytes.NewReader(body))
	req.Header.Set("Authorization", "key="+serverKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[FCM] push error: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("[FCM] push HTTP status: %d", resp.StatusCode)
	}
}
