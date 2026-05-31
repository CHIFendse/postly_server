package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2/google"
)

const fcmTokenPrefix = "fcm:user:"

// ── FCM HTTP v1 init ──────────────────────────────────────────────────────────
// Uses FIREBASE_CREDENTIALS env var — paste the full content of serviceAccountKey.json.
// Old FCM Legacy API (fcm.googleapis.com/fcm/send with FCM_SERVER_KEY) was
// shut down by Google on June 20 2024. This uses the current HTTP v1 API.

var (
	fcmProjectID  string
	fcmGetToken   func(ctx context.Context) (string, error)
	fcmOnce       sync.Once
)

func initFCM() {
	fcmOnce.Do(func() {
		creds := os.Getenv("FIREBASE_CREDENTIALS")
		if creds == "" {
			log.Println("[FCM] FIREBASE_CREDENTIALS not set — push disabled")
			return
		}

		// Parse project_id from service account JSON
		var sa struct {
			ProjectID string `json:"project_id"`
		}
		if err := json.Unmarshal([]byte(creds), &sa); err != nil || sa.ProjectID == "" {
			log.Printf("[FCM] bad service account JSON: %v", err)
			return
		}
		fcmProjectID = sa.ProjectID

		// Build OAuth2 token source (cached & auto-refreshed)
		ts, err := google.CredentialsFromJSON(
			context.Background(),
			[]byte(creds),
			"https://www.googleapis.com/auth/firebase.messaging",
		)
		if err != nil {
			log.Printf("[FCM] credentials error: %v", err)
			return
		}

		fcmGetToken = func(ctx context.Context) (string, error) {
			t, err := ts.TokenSource.Token()
			if err != nil {
				return "", err
			}
			return t.AccessToken, nil
		}
		log.Printf("[FCM] v1 API ready, project=%s", fcmProjectID)
	})
}

// send posts one FCM v1 message.
func fcmSend(ctx context.Context, message map[string]any) {
	initFCM()
	if fcmGetToken == nil {
		return
	}

	token, err := fcmGetToken(ctx)
	if err != nil {
		log.Printf("[FCM] get token: %v", err)
		return
	}

	body, _ := json.Marshal(map[string]any{"message": message})
	url := fmt.Sprintf(
		"https://fcm.googleapis.com/v1/projects/%s/messages:send",
		fcmProjectID,
	)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[FCM] send: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("[FCM] HTTP %d for request to %s", resp.StatusCode, url)
	}
}

func getFCMToken(ctx context.Context, cache *redis.Client, userID string) string {
	t, _ := cache.Get(ctx, fcmTokenPrefix+userID).Result()
	return t
}

// ── HTTP handler ──────────────────────────────────────────────────────────────

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

// ── Push senders ──────────────────────────────────────────────────────────────

// sendCallPush — HIGH-priority notification that wakes the screen.
// Triggers the full-screen incoming call UI on the device.
func sendCallPush(ctx context.Context, cache *redis.Client, recipientID, callerName, chatID string) {
	deviceToken := getFCMToken(ctx, cache, recipientID)
	if deviceToken == "" {
		return
	}
	fcmSend(ctx, map[string]any{
		"token": deviceToken,
		"notification": map[string]string{
			"title": "Входящий звонок",
			"body":  callerName + " звонит вам",
		},
		"android": map[string]any{
			"priority": "HIGH",
			"notification": map[string]any{
				"channel_id": "postly_calls", // matches client PostlyPlugin channel
				"sound":      "default",
				"visibility": "PUBLIC",
			},
		},
		"apns": map[string]any{
			"headers": map[string]string{"apns-priority": "10"},
			"payload": map[string]any{
				"aps": map[string]any{
					"alert": map[string]string{
						"title": "Входящий звонок",
						"body":  callerName + " звонит вам",
					},
					"sound":             "default",
					"category":          "CALL",
					"content-available": 1,
				},
			},
		},
		"data": map[string]string{
			"type":    "CALL_INVITE",
			"chat_id": chatID,
			"name":    callerName,
		},
	})
}

// sendMessagePush — normal-priority notification for a new message.
// Delivers to background/killed users who missed the WebSocket broadcast.
func sendMessagePush(ctx context.Context, cache *redis.Client, recipientID, senderName, text, chatID string) {
	deviceToken := getFCMToken(ctx, cache, recipientID)
	if deviceToken == "" {
		return
	}
	preview := text
	if len(preview) > 120 {
		preview = preview[:120] + "..."
	}
	fcmSend(ctx, map[string]any{
		"token": deviceToken,
		"notification": map[string]string{
			"title": senderName,
			"body":  preview,
		},
		"android": map[string]any{
			"priority": "NORMAL",
			"notification": map[string]any{
				"channel_id": "postly_messages", // matches client PostlyPlugin channel
				"sound":      "default",
			},
		},
		"apns": map[string]any{
			"payload": map[string]any{
				"aps": map[string]any{
					"alert": map[string]string{
						"title": senderName,
						"body":  preview,
					},
					"sound": "default",
					"badge": 1,
				},
			},
		},
		"data": map[string]string{
			"type":    "NEW_MESSAGE",
			"chat_id": chatID,
			"text":    preview,
		},
	})
}
