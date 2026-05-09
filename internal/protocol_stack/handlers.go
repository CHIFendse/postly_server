package protocol_stack

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
    "github.com/gorilla/websocket"
    "sync"
    "os"
    "github.com/golang-jwt/jwt/v5"
)
const (
    TypeNewMessage = "NEW_MESSAGE"
    TypeCallInvite = "CALL_INVITE"
    TypeCallAccept = "CALL_ACCEPT"
    TypeCallReject = "CALL_REJECT"
    TypeCallHangup = "CALL_HANGUP"
)
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
        // --- ЛОГИКА ЗВОНКОВ (Signaling) ---
        if msgType == TypeCallInvite || msgType == TypeCallAccept || msgType == TypeCallReject || msgType == TypeCallHangup {
            // Сигналы звонка МЫ НЕ СОХРАНЯЕМ В БД. Просто пересылаем.
            log.Printf("Call Signal: %s from %s in chat %s", msgType, userID, chatID)
            broadcastToOtherParticipants(chatID, userID, p)
            continue
        }

        // --- ЛОГИКА ОБЫЧНЫХ СООБЩЕНИЙ ---
        text, _ := raw["text"].(string)
        if text == "" && msgType == "" { continue }

        // Сохраняем в БД только реальные сообщения
        id, err := repo.AddMessage(chatID, userID, text)
        if err != nil {
            log.Printf("DB Error: %v", err)
            continue
        }
        // Формируем пакет для рассылки
        broadcastData := map[string]interface{}{
            "type":      TypeNewMessage,
            "id":        id,
            "chat_id":   chatID,
            "sender_id": userID,
            "text":      text,
        }
        finalPayload, _ := json.Marshal(broadcastData)
        broadcastToOtherParticipants(chatID, userID, finalPayload)
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
        fmt.Println("Ошибка:", err.Error())
        return
    }

    // Вызываем метод из твоего auth_service.go
    err := authService.Register(data.Username, data.Password, data.Email, data.Phone)
    if err != nil {
        fmt.Println("Ошибка:", err.Error())
        w.WriteHeader(http.StatusConflict)
        json.NewEncoder(w).Encode(map[string]error{"error": err})
        return
    }

    w.WriteHeader(http.StatusCreated)
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

    // Вызываем метод из твоего auth_service.go
    resp, err := repo.GetChats(data.Id)
    if err != nil {
        w.WriteHeader(http.StatusConflict)
        json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
        return
    }

    w.WriteHeader(http.StatusOK)
    if err := json.NewEncoder(w).Encode(resp); err != nil {
        // Логируем ошибку, если не удалось отправить JSON
        return
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

    resp, err := repo.GetGroups(data.Id)
    if err != nil {
        w.WriteHeader(http.StatusConflict)
        json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
        return
    }

    w.WriteHeader(http.StatusOK)
    if err := json.NewEncoder(w).Encode(resp); err != nil {
        // Логируем ошибку, если не удалось отправить JSON
        return
    }
}


func broadcastToOtherParticipants(chatID string, senderID string, payload []byte) {
    // 1. Получаем список участников из БД
    participants, err := repo.GetChatParticipants(chatID)
    if err != nil {
        return
    }
    for _, pID := range participants {
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
