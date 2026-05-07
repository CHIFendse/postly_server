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
            log.Printf("WS Read Error from %s: %v", userID, err)
            break
        }

        var msgData struct {
            SenderID string `json:"sender_id"`
            Text     string `json:"text"`
            ChatID   string `json:"chat_id"`
        }

        if err := json.Unmarshal(p, &msgData); err != nil {
            log.Printf("WS JSON Error: %v", err)
            continue
        }

        // Динамическая проверка комнаты (на случай если чат только что создали)
        roomsMu.Lock()
        if rooms[msgData.ChatID] == nil {
            rooms[msgData.ChatID] = make(map[string]*websocket.Conn)
        }
        rooms[msgData.ChatID][userID] = conn
        roomsMu.Unlock()

        // 5. Сохранение сообщения в БД
        newMsg, err := repo.AddMessage(msgData.ChatID, userID, msgData.Text)
        if err != nil {
            log.Printf("DB Error (AddMessage): %v", err)
            continue
        }

        // 6. Подготовка пакета для рассылки
        finalPayload, _ := json.Marshal(map[string]interface{}{
            "type": "NEW_MESSAGE",
            "data": newMsg, // Здесь структура сообщения из БД (id, text, chat_id, etc.)
        })

        // 7. УМНАЯ РАССЫЛКА
        // Сначала рассылаем тем, кто уже "сидит" в комнате в памяти
        roomsMu.Lock()
        recipientsInRoom := make(map[string]bool)
        if clients, ok := rooms[msgData.ChatID]; ok {
            for rID, clientConn := range clients {
                clientConn.WriteMessage(websocket.TextMessage, finalPayload)
                recipientsInRoom[rID] = true
            }
        }
        roomsMu.Unlock()

        // Теперь пытаемся достучаться до остальных участников, которые онлайн,
        // но еще не в комнате (важно для новых чатов)
        participants, err := repo.GetChatParticipants(msgData.ChatID)
        if err == nil {
            for _, pID := range participants {
                // Если мы ему еще не отправили через комнату
                if !recipientsInRoom[pID] {
                    userConnsMu.Lock()
                    if targetConn, online := userConns[pID]; online {
                        targetConn.WriteMessage(websocket.TextMessage, finalPayload)
                        
                        // Заодно "подписываем" его на комнату на будущее
                        roomsMu.Lock()
                        rooms[msgData.ChatID][pID] = targetConn
                        roomsMu.Unlock()
                    }
                    userConnsMu.Unlock()
                }
            }
        }
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