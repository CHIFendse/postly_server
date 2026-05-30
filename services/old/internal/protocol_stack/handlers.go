package protocol_stack

import (
	"backend/internal/cache"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

const (
	TypeNewMessage    = "NEW_MESSAGE"
	TypeCallInvite    = "CALL_INVITE"
	TypeCallAccept    = "CALL_ACCEPT"
	TypeCallReject    = "CALL_REJECT"
	TypeCallHangup    = "CALL_HANGUP"
	TypeTyping        = "TYPING"
	TypeFriendRequest = "FRIEND_REQUEST"
)

// typingCache: "chatId:userId" → username. TTL 4s — автоматически истекает когда перестали печатать.
var typingCache = cache.New[string]("typing")
//-----------------------------------------------------РАБОТА С WEBSOCKET----------------------------------------------------
var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true
    },
}

var (
    rooms   = make(map[string]map[string]*websocket.Conn)
    roomsMu sync.Mutex
)

var (
    userConns   = make(map[string]*websocket.Conn)
    userConnsMu sync.Mutex
)

func getJwtKey() []byte {
    return []byte(os.Getenv("JWT_SECRET"))
}

type Claims struct {
    UserID string `json:"user_id"`
    jwt.RegisteredClaims
}


func broadcast(chatID string, message []byte) {
    roomsMu.Lock()
    defer roomsMu.Unlock()
    
    if clients, ok := rooms[chatID]; ok {
        for id, conn := range clients {
            log.Printf("Message was delivered into chat %s", chatID)
            err := conn.WriteMessage(websocket.TextMessage, message)
            if err != nil {
                log.Printf("Message not delivered %s: %v", id, err)
                conn.Close()
                delete(clients, id) // Удаляем «мертвое» соединение
            }
        }
    }
}

func handleWS(w http.ResponseWriter, r *http.Request) {
    // 1. Настройка Upgrader и проверка авторизации
    upgrader.CheckOrigin = func(r *http.Request) bool { return true }

    userID, ok := r.Context().Value(UserIDKey).(string)
    if !ok || userID == "" {
        log.Printf("WS Error: Unauthorized access attempt")
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Printf("WS Upgrade Error: %v", err)
        return
    }

    // 2. Глобальная регистрация соединения
    userConnsMu.Lock()
    userConns[userID] = conn
    userConnsMu.Unlock()

    // 3. Первичная подписка на существующие комнаты
    // Загружаем чаты пользователя, чтобы он ловил сообщения в них сразу после входа
    userChats, err := repo.GetChats(userID)
    if err == nil {
        roomsMu.Lock()
        for _, chat := range userChats {
            cID := chat.Id
            if rooms[cID] == nil {
                rooms[cID] = make(map[string]*websocket.Conn)
            }
            rooms[cID][userID] = conn
        }
        roomsMu.Unlock()
    }
    userGroups, err := repo.GetGroups(userID)
    if err == nil {
        roomsMu.Lock()
        for _, group := range userGroups {
            gID := group.Id
            if rooms[gID] == nil {
                rooms[gID] = make(map[string]*websocket.Conn)
            }
            rooms[gID][userID] = conn
        }
        roomsMu.Unlock()
    }
    // Очистка при отключении
    defer func() {
        userConnsMu.Lock()
        delete(userConns, userID)
        userConnsMu.Unlock()

        roomsMu.Lock()
        for _, chat := range userChats {
            if clients, ok := rooms[chat.Id]; ok {
                delete(clients, userID)
                if len(clients) == 0 {
                    delete(rooms, chat.Id)
                }
            }
        }
        for _, group := range userGroups {
            if clients, ok := rooms[group.Id]; ok {
                delete(clients, userID)
                if len(clients) == 0 {
                    delete(rooms, group.Id)
                }
            }
        }
        roomsMu.Unlock()
        conn.Close()
        log.Printf("WS: User %s disconnected", userID)
    }()

    // 4. Основной цикл прослушивания сообщений
    for {
        _, p, err := conn.ReadMessage()
        if err != nil {
            break
        }
        
        // Сначала парсим заголовок, чтобы понять тип
        var raw map[string]interface{}
        if err := json.Unmarshal(p, &raw); err != nil {
            continue
        }
        msgType, _ := raw["type"].(string)
        chatID, _ := raw["chat_id"].(string)
        // --- СТАТУС ПЕЧАТАНИЯ ---
        if msgType == TypeTyping {
            senderName, _ := raw["username"].(string)
            if chatID != "" && senderName != "" {
                typingCache.Set(chatID+":"+userID, senderName, 4*time.Second)
                payload, _ := json.Marshal(map[string]interface{}{
                    "type":      TypeTyping,
                    "chat_id":   chatID,
                    "sender_id": userID,
                    "username":  senderName,
                })
                // skipIfInRoom=false: broadcast() не вызывается перед этим,
                // поэтому надо слать всем участникам, включая тех кто в rooms.
                broadcastToOtherParticipants(chatID, userID, payload, false)
            }
            continue
        }

        // --- ЛОГИКА ЗВОНКОВ (Signaling) ---
        if msgType == TypeCallInvite || msgType == TypeCallAccept || msgType == TypeCallReject || msgType == TypeCallHangup {
            log.Printf("Call Signal: %s from %s in chat %s", msgType, userID, chatID)
            broadcastToOtherParticipants(chatID, userID, p, false)
            continue
        }

        // --- ЛОГИКА ОБЫЧНЫХ СООБЩЕНИЙ ---
        text, _ := raw["text"].(string)
        if text == "" && msgType == "" { continue }
        senderName, _ := raw["username"].(string)

        id, err := repo.AddMessage(chatID, userID, text)
        if err != nil {
            log.Printf("DB Error: %v", err)
            continue
        }

        now := time.Now()
        broadcastData := map[string]interface{}{
            "type":       TypeNewMessage,
            "id":         id,
            "chat_id":    chatID,
            "sender_id":  userID,
            "text":       text,
            "username":   senderName,
            "updated_at": now.Unix(),
            "created_at": now.Format(time.RFC3339),
        }

        log.Printf("Broadcasting message: chatID=%s, text=%s", chatID, text)

        finalPayload, _ := json.Marshal(broadcastData)

        // broadcast — всем в rooms (включая отправителя).
        // broadcastToOtherParticipants(skipIfInRoom=true) — fallback только для тех,
        // кто не в rooms, чтобы не слать дубль.
        broadcast(chatID, finalPayload)
        broadcastToOtherParticipants(chatID, userID, finalPayload, true)

        // Эхо отправителю если он не в rooms (GetGroups/GetChats упал при подключении).
        // Без эха chatsMenu не обновит last_message в реальном времени.
        roomsMu.Lock()
        _, senderInRoom := rooms[chatID][userID]
        roomsMu.Unlock()
        if !senderInRoom {
            userConnsMu.Lock()
            if sConn, ok := userConns[userID]; ok {
                sConn.WriteMessage(websocket.TextMessage, finalPayload) //nolint
            }
            userConnsMu.Unlock()
        }
    }
}

//--------------------------------------------------РАБОТА С HTTP ЗАПРОСАМИ----------------------------------------------------
func handleRegister(w http.ResponseWriter, r *http.Request) {
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

    // Вызываем метод из твоего auth_service.go
    err := authService.Register(data.Username, data.Password, data.Email, data.Phone)
    if err != nil {
        w.Header().Set("Content-Type", "application/json")
        errMsg := "Ошибка регистрации"
        fmt.Println(err.Error())
        if strings.Contains(err.Error(), "users_email_key") && strings.Contains(err.Error(), "23505"){
            errMsg = "Пользователь с такой почтой уже существует"
        } else if strings.Contains(err.Error(), "users_username_key")  && strings.Contains(err.Error(), "23505") {
            errMsg = "Пользователь с таким именем уже существует"
        }
        w.WriteHeader(http.StatusConflict)
        json.NewEncoder(w).Encode(map[string]string{"message": errMsg})
        return
    }

    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(map[string]bool{"status": true})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    var data struct {
        Username string `json:"username"`
        Password string `json:"password"`
    }

    if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
        http.Error(w, "Bad request", http.StatusBadRequest)
        return
    }

    // Вызываем метод Login и получаем токен
    token, err := authService.Login(data.Username, data.Password)
    if err != nil {
        w.WriteHeader(http.StatusUnauthorized)
        json.NewEncoder(w).Encode(map[string]string{"message": "Неверный логин или пароль"})
        return
    }
    id, err := repo.GetId(data.Username)
    if err != nil {
        fmt.Println(err)
        w.WriteHeader(http.StatusUnauthorized)
        json.NewEncoder(w).Encode(map[string]string{"message": "Пользователь не найден"})
        return
    }
    // Отправляем JWT токен клиенту
    json.NewEncoder(w).Encode(map[string]interface{}{"token": token, "username": data.Username, "id":id})
}



func handleVerify(w http.ResponseWriter, r *http.Request) {
    // Получаем токен из заголовка Authorization: Bearer <token>
    authHeader := r.Header.Get("Authorization")
    if authHeader == "" {
        http.Error(w, "Missing token", http.StatusUnauthorized)
        return
    }

    tokenString := strings.TrimPrefix(authHeader, "Bearer ")
    
    // Используем метод ValidateToken из твоего AuthService
    isValid, err := authService.ValidateToken(tokenString)
    if err != nil || !isValid {
        w.WriteHeader(http.StatusUnauthorized)
        json.NewEncoder(w).Encode(map[string]string{"message": "Invalid token"})
        return
    }

    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(map[string]bool{"status": true})
}


func handleGetMessages(w http.ResponseWriter, r *http.Request){
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    var data struct {
        Chat_id string `json:"chat_id"`
    }

    if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
        http.Error(w, "Bad request", http.StatusBadRequest)
        return
    }

    resp, err := repo.GetMessages(data.Chat_id)
    if err != nil {
        log.Printf("get messages error %s: %v", data.Chat_id, err)
        w.WriteHeader(http.StatusInternalServerError)
        json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
        return
    }

    w.WriteHeader(http.StatusOK)
    if err := json.NewEncoder(w).Encode(resp); err != nil {
        return
    }
}
func handleGetChats(w http.ResponseWriter, r *http.Request){
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    var data struct {
        Id string `json:"id"`
    }

    if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
        http.Error(w, "Bad request", http.StatusBadRequest)
        return
    }

    resp, err := repo.GetChats(data.Id)
    if err != nil {
        log.Printf("GetChats Error: %v", err)
        w.WriteHeader(http.StatusInternalServerError)
        json.NewEncoder(w).Encode(map[string]string{"error": "Could not get chats"})
        return
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    if err := json.NewEncoder(w).Encode(resp); err != nil {
        log.Printf("GetChats encode error: %v", err)
    }
}


func handleAddMessage(w http.ResponseWriter, r *http.Request){
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    var data struct {
        Sender_id string `json:"sender_id"`
        Text string `json:"text"`
        Chat_id string `json:"chat_id"`
    }

    if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
        http.Error(w, "Bad request", http.StatusBadRequest)
        return
    }

    resp, err := repo.AddMessage(data.Chat_id, data.Sender_id, data.Text)
    if err != nil {
        w.WriteHeader(http.StatusConflict)
        json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
        return
    }

    w.WriteHeader(http.StatusOK)
    if err := json.NewEncoder(w).Encode(map[string]string{"id": resp}); err != nil {
        log.Printf("Error encoding response: %v", err)
    }
}

func handleCreateChat(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    var data struct {
        UserId string `json:"user_id"`
        TargetUsername string `json:"target_username"`
    }

    if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
        http.Error(w, "Bad request", http.StatusBadRequest)
        return
    }

    createdChatID, targetID, err := repo.CreateNewChat(data.UserId, data.TargetUsername)
    if err != nil {
        log.Printf("CreateChat Error: %v", err)
        w.WriteHeader(http.StatusConflict)
        json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
        return
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(map[string]string{"id": createdChatID})

    // РЕГИСТРАЦИЯ В ПАМЯТИ (ROOMS)
    roomsMu.Lock()
    if rooms[createdChatID] == nil {
        rooms[createdChatID] = make(map[string]*websocket.Conn)
    }
    
    userConnsMu.Lock()
    // 1. Регистрируем создателя (он точно онлайн, раз отправил запрос)
    if creatorConn, ok := userConns[data.UserId]; ok {
        rooms[createdChatID][data.UserId] = creatorConn
    }

    // 2. Проверяем таргет и уведомляем его
    if targetConn, ok := userConns[targetID]; ok {
        rooms[createdChatID][targetID] = targetConn
        
        notification := map[string]interface{}{
            "type": "NEW_CHAT",
            "data": map[string]string{
                "chat_id": createdChatID,
            },
        }
        
        err := targetConn.WriteJSON(notification)
        if err != nil {
            log.Printf("Failed to send notification: %v", err)
            targetConn.Close()
            delete(userConns, targetID)
        }
    }
    userConnsMu.Unlock()
    roomsMu.Unlock()
}

func handleCreateGroup(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    var data struct {
        AdminId   string   `json:"admin_id"`
        Name      string   `json:"name"`
        IsPrivate bool     `json:"is_private"`
        Members   []string `json:"members"`
    }

    if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
        http.Error(w, "Bad request", http.StatusBadRequest)
        return
    }

    if data.AdminId == "" || data.Name == "" {
        http.Error(w, "Missing required fields", http.StatusBadRequest)
        return
    }
    fmt.Println(data.Members)
    createdGroupID, err := repo.CreateNewGroup(data.Name, data.AdminId, data.IsPrivate, data.Members)
    if err != nil {
        log.Printf("CreateGroup Error: %v", err)
        w.WriteHeader(http.StatusInternalServerError)
        json.NewEncoder(w).Encode(map[string]string{"error": "Could not create group"})
        return
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)
    json.NewEncoder(w).Encode(map[string]string{
        "id":   createdGroupID,
        "name": data.Name,
    })

    roomsMu.Lock()
    defer roomsMu.Unlock()
    if rooms[createdGroupID] == nil {
        rooms[createdGroupID] = make(map[string]*websocket.Conn)
    }
    userConnsMu.Lock()
    if adminConn, ok := userConns[data.AdminId]; ok {
        rooms[createdGroupID][data.AdminId] = adminConn
    }
    userConnsMu.Unlock()
}

func handleGetGroups(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    var data struct {
        Id string `json:"id"`
    }

    if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
        http.Error(w, "Bad request", http.StatusBadRequest)
        return
    }

    log.Printf("GetGroups request for user: %s", data.Id)

    resp, err := repo.GetGroups(data.Id)
    if err != nil {
        log.Printf("GetGroups Error: %v", err)
        w.WriteHeader(http.StatusInternalServerError)
        json.NewEncoder(w).Encode(map[string]string{"error": "Could not get groups"})
        return
    }

    log.Printf("GetGroups found %d groups", len(resp))

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    if err := json.NewEncoder(w).Encode(resp); err != nil {
        log.Printf("GetGroups encode error: %v", err)
    }
}

func broadcastToOtherParticipants(chatID string, senderID string, payload []byte, skipIfInRoom bool) {
    participants, err := repo.GetChatParticipants(chatID)
    if err != nil {
        participants, err = repo.GetGroupParticipants(chatID)
        if err != nil {
            log.Printf("broadcastToOtherParticipants error: %v", err)
            return
        }
    }

    // Снимаем снимок rooms, чтобы не слать дубль тем, кто уже получил через broadcast().
    roomsMu.Lock()
    inRoom := make(map[string]bool, len(rooms[chatID]))
    for id := range rooms[chatID] {
        inRoom[id] = true
    }
    roomsMu.Unlock()

    for _, pID := range participants {
        if pID == senderID {
            continue
        }
        if skipIfInRoom && inRoom[pID] {
            continue
        }
        userConnsMu.Lock()
        if targetConn, online := userConns[pID]; online {
            err := targetConn.WriteMessage(websocket.TextMessage, payload)
            if err != nil {
                log.Printf("Send Error to %s: %v", pID, err)
            }
        }
        userConnsMu.Unlock()
    }
}

// ── MESSAGE ACTIONS ──────────────────────────────────────────────────────────

func handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	senderID, _ := r.Context().Value(UserIDKey).(string)

	var data struct {
		MessageID string `json:"message_id"`
		UserID    string `json:"user_id"`
		ChatID    string `json:"chat_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	if senderID == "" {
		senderID = data.UserID
	}

	chatID, newLastMsg, newLastUsername, err := repo.DeleteMessage(data.MessageID, senderID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
		return
	}

	// Broadcast удаления всем участникам (включая обновлённый last_message)
	payload, _ := json.Marshal(map[string]interface{}{
		"type":         "DELETE_MESSAGE",
		"message_id":   data.MessageID,
		"chat_id":      chatID,
		"last_message": newLastMsg,
		"username":     newLastUsername,
	})
	broadcast(chatID, payload)
	broadcastToOtherParticipants(chatID, senderID, payload, true)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func handleClearChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, _ := r.Context().Value(UserIDKey).(string)

	var data struct {
		ChatID string `json:"chat_id"`
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	if userID == "" {
		userID = data.UserID
	}

	if err := repo.ClearChat(data.ChatID, userID); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
		return
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"type":    "CLEAR_CHAT",
		"chat_id": data.ChatID,
	})
	broadcast(data.ChatID, payload)
	broadcastToOtherParticipants(data.ChatID, userID, payload, true)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func handleDeleteChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, _ := r.Context().Value(UserIDKey).(string)

	var data struct {
		ChatID string `json:"chat_id"`
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	if userID == "" {
		userID = data.UserID
	}

	// Уведомляем участников до удаления
	payload, _ := json.Marshal(map[string]interface{}{
		"type":    "DELETE_CHAT",
		"chat_id": data.ChatID,
	})
	broadcast(data.ChatID, payload)
	broadcastToOtherParticipants(data.ChatID, userID, payload, true)

	// Удаляем room из памяти
	roomsMu.Lock()
	delete(rooms, data.ChatID)
	roomsMu.Unlock()

	if err := repo.DeleteChat(data.ChatID, userID); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// ── FRIENDS ─────────────────────────────────────────────────────────────────

func handleSendFriendRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	senderID, _ := r.Context().Value(UserIDKey).(string)

	var data struct {
		UserID         string `json:"user_id"`
		TargetUsername string `json:"target_username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	if data.TargetUsername == "" {
		http.Error(w, "target_username required", http.StatusBadRequest)
		return
	}
	// Используем userID из токена (не из тела запроса — защита от подмены)
	if senderID == "" {
		senderID = data.UserID
	}

	reqID, receiverID, err := repo.SendFriendRequest(senderID, data.TargetUsername)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
		return
	}

	// Уведомляем получателя через WS если он онлайн
	senderUsername, _ := repo.GetUserFromID(senderID)
	notification, _ := json.Marshal(map[string]interface{}{
		"type":          TypeFriendRequest,
		"id":            reqID,
		"request_id":    reqID,
		"sender_id":     senderID,
		"username":      senderUsername,
		"from_username": senderUsername,
	})
	userConnsMu.Lock()
	if rc, ok := userConns[receiverID]; ok {
		rc.WriteMessage(websocket.TextMessage, notification) //nolint
	}
	userConnsMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"request_id": reqID})
}

func handleGetFriendRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, _ := r.Context().Value(UserIDKey).(string)

	var data struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	if userID == "" {
		userID = data.UserID
	}

	requests, err := repo.GetFriendRequests(userID)
	if err != nil {
		log.Printf("GetFriendRequests error: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(requests)
}

func handleAcceptFriendRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, _ := r.Context().Value(UserIDKey).(string)

	var data struct {
		UserID    string `json:"user_id"`
		RequestID string `json:"request_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	if userID == "" {
		userID = data.UserID
	}

	if err := repo.AcceptFriendRequest(userID, data.RequestID); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func handleDeclineFriendRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, _ := r.Context().Value(UserIDKey).(string)

	var data struct {
		UserID    string `json:"user_id"`
		RequestID string `json:"request_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	if userID == "" {
		userID = data.UserID
	}

	if err := repo.DeclineFriendRequest(userID, data.RequestID); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func handleGetFriends(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, _ := r.Context().Value(UserIDKey).(string)

	var data struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	if userID == "" {
		userID = data.UserID
	}

	friends, err := repo.GetFriends(userID)
	if err != nil {
		log.Printf("GetFriends error: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(friends)
}

func handleHTTP(w http.ResponseWriter, r *http.Request) {
    if r.Method == http.MethodGet {
        w.Header().Set("Content-Type", "application/json")
        
        response := Response{Status: true}

        err := json.NewEncoder(w).Encode(response)
        if err != nil {
            http.Error(w, "error encoding JSON", http.StatusInternalServerError)
            return
        }
    }
}
