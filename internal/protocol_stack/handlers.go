package protocol_stack

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)


func handleHTTP(w http.ResponseWriter, r *http.Request) {
    if r.Method == http.MethodGet {
        w.Header().Set("Content-Type", "application/json")
        
        response := Response{Status: true}

        err := json.NewEncoder(w).Encode(response)
        if err != nil {
            http.Error(w, "Ошибка кодирования JSON", http.StatusInternalServerError)
            return
        }
    }
}

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
        log.Printf("Ошибка при получении сообщений для чата %s: %v", data.Chat_id, err)
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