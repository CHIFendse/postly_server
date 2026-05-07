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
    upgrader.CheckOrigin = func(r *http.Request) bool { return true }

    userID, ok := r.Context().Value(UserIDKey).(string)
    if !ok || userID == "" {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        return
    }

    // 1. Глобальная регистрация
    userConnsMu.Lock()
    userConns[userID] = conn
    userConnsMu.Unlock()

    // 2. РЕГИСТРАЦИЯ В КОМНАТАХ (Делаем ОДИН РАЗ при подключении)
    // Достаем все чаты, в которых состоит пользователь, и подписываем его на них
    userChats, _ := repo.GetChats(userID) // добавь пустой токен или как там у тебя в методе
    roomsMu.Lock()
    for _, chat := range userChats {
        cID := chat.Id
        if rooms[cID] == nil {
            rooms[cID] = make(map[string]*websocket.Conn)
        }
        rooms[cID][userID] = conn
    }
    roomsMu.Unlock()

    defer func() {
        userConnsMu.Lock()
        delete(userConns, userID)
        userConnsMu.Unlock()

        // Чистим пользователя из всех комнат при выходе
        roomsMu.Lock()
        for _, chat := range userChats {
            cID := chat.Id
            if clients, ok := rooms[cID]; ok {
                delete(clients, userID)
            }
        }
        roomsMu.Unlock()
        conn.Close()
    }()

    // 3. ЦИКЛ ОБРАБОТКИ
    for {
        _, p, err := conn.ReadMessage()
        if err != nil {
            break
        }

        var msgData struct {
            SenderID string `json:"sender_id"`
            Text     string `json:"text"`
            ChatID   string `json:"chat_id"`
        }

        if err := json.Unmarshal(p, &msgData); err != nil {
            continue
        }

        // Сохраняем в БД
        newMsg, err := repo.AddMessage(msgData.ChatID, userID, msgData.Text)
        if err != nil {
            continue
        }

        // Формируем пакет
        finalPayload, _ := json.Marshal(map[string]interface{}{
            "type": "NEW_MESSAGE",
            "data": newMsg,
        })

        // 4. РАССЫЛКА (Broadcast)
        // Отправляем всем, кто сейчас "подписан" на эту комнату
        roomsMu.Lock()
        if clients, ok := rooms[msgData.ChatID]; ok {
            for _, clientConn := range clients {
                clientConn.WriteMessage(websocket.TextMessage, finalPayload)
            }
        } else {
            // Если комнаты нет в памяти (например, первый месседж), 
            // отправляем хотя бы себе
            conn.WriteMessage(websocket.TextMessage, finalPayload)
        }
        roomsMu.Unlock()
    }
}


//--------------------------------------------------РАБОТА С HTTP ЗАПРОСАМИ----------------------------------------------------
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

func handleRegister(w http.ResponseWriter, r *http.Request) {
    fmt.Println("тут есть")
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
        json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
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

    userConnsMu.Lock()
    if conn, ok := userConns[targetID]; ok {
        notification := map[string]interface{}{
            "type": "NEW_CHAT",
            "data": map[string]string{
                "chat_id": createdChatID,
            },
        }
        err := conn.WriteJSON(notification)
        if err != nil {
            log.Printf("Failed to send WS notification to %s: %v", targetID, err)
            // Если соединение битое, лучше его закрыть/удалить
            conn.Close()
            delete(userConns, targetID)
        }
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