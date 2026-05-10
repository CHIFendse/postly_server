package database

import (
	"database/sql"
	"time"
	"fmt"
)

type Repository struct {
    db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
    return &Repository{db: db}
}

type Chats struct {
	Id            string          `json:"id"`
	Name          string          `json:"name"`
    LastMsg       string          `json:"last_message"`
    LastMsgSender string          `json:"username"`
    UpdatedAt      int             `json:"updated_at"`
}

type Groups struct {
    Id        string       `json:"id"`
    Name      string    `json:"name"`
    CreatedAt time.Time `json:"created_at"`
}

type Messages struct {
	Id string `json:"id"`
	Text string `json:"text"`
	Chat_id string `json:"chat_id"`
	Sender_id string `json:"sender_id"`
	Created_at time.Time `json:"created_at"`
}

func (c *Repository)GetGroups(id string) ([]*Groups, error){
	query := `
        SELECT g.id, g.name, g.created_at 
        FROM groups g
        JOIN group_members gm ON g.id = gm.group_id
        WHERE gm.user_id = $1
    `

    rows, err := c.db.Query(query, id)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var groups []*Groups
    for rows.Next() {
        g := &Groups{}
        err := rows.Scan(&g.Id, &g.Name, &g.CreatedAt)
        if err != nil {
            return nil, err
        }
        groups = append(groups, g)
    }

    // Если групп нет, возвращаем пустой слайс вместо nil для корректного JSON []
    if groups == nil {
        groups = []*Groups{}
    }

    return groups, nil
}
func (c *Repository)GetMessages(chat_id string) ([]*Messages, error){
	query := `SELECT id, text, conversation_id, sender_id, created_at FROM messages WHERE conversation_id = $1`
	rows, err := c.db.Query(query, chat_id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := make([]*Messages, 0)
	for rows.Next() {
		m := new(Messages)
		err := rows.Scan(&m.Id, &m.Text, &m.Chat_id, &m.Sender_id, &m.Created_at)
		if err != nil{
			return nil, err
		}
		messages = append(messages, m)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return messages, nil
}

func (c *Repository) GetChats(id string) ([]*Chats, error){
	query := `
        SELECT 
            c.id, 
            u.username AS chat_name, 
            COALESCE(c.last_message, ''), 
            COALESCE(sender.username, ''),
            EXTRACT(EPOCH FROM (c.updated_at AT TIME ZONE 'Europe/Moscow' AT TIME ZONE 'UTC'))::INT
        FROM chats c 
        JOIN users u ON u.id = (
            CASE 
                WHEN c.user_id1 = $1::uuid THEN c.user_id2
                ELSE c.user_id1 
            END
        )::uuid -- Явное приведение результата CASE к UUID
        LEFT JOIN users sender ON sender.id = c.last_msg_sender::uuid -- Приведение отправителя
        WHERE c.user_id1 = $1::uuid OR c.user_id2 = $1::uuid 
        ORDER BY c.updated_at DESC;`

    // Остальной код без изменений...
    rows, err := c.db.Query(query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	chats := []*Chats{} 
    
    for rows.Next(){
        m := new(Chats)
        // Теперь сканируем 5 полей
        err := rows.Scan(&m.Id, &m.Name, &m.LastMsg, &m.LastMsgSender, &m.UpdatedAt)
        if err != nil {
            fmt.Println("Ошибка Scan:", err)
            return nil, err
        } 
        chats = append(chats, m)
    }

    return chats, nil
}

func (c *Repository) AddMessage(chat_id, sender_id, text string) (string, error) {
    tx, err := c.db.Begin()
    if err != nil {
        return "", err
    }
    defer tx.Rollback() 

    // 1. МЕНЯЕМ ТИП НА string, так как в базе это UUID
    var newID string 
    query := `INSERT INTO messages (conversation_id, sender_id, text) VALUES($1, $2, $3) RETURNING id;`
    
    // 2. Сканируем сразу в строку
    err = tx.QueryRow(query, chat_id, sender_id, text).Scan(&newID)
    if err != nil {
        fmt.Println("Ошибка добавления сообщения:", err)
        return "", err
    }

    _, err = tx.Exec(`
        UPDATE chats 
        SET last_message = $1, last_msg_sender = $2, updated_at = NOW()
        WHERE id = $3`, 
        text, sender_id, chat_id)
    if err != nil {
        return "", err
    }

    err = tx.Commit()
    if err != nil {
        return "", err
    }

    // 3. Возвращаем уже готовую строку UUID
    return newID, nil
}

func (r *Repository) CreateNewChat(userID string, targetUsername string) (string, string, error) {
    var targetUserID string
    // 1. Ищем ID собеседника
    err := r.db.QueryRow("SELECT id FROM users WHERE LOWER(username) = LOWER($1)", targetUsername).Scan(&targetUserID)
    if err != nil {
        return "", "", err
    }

    u1, u2 := userID, targetUserID
    if u1 > u2 { u1, u2 = u2, u1 }

    // 2. Проверяем, существует ли уже чат между этими пользователями
    var existingID string
    err = r.db.QueryRow("SELECT id FROM chats WHERE user_id1 = $1 AND user_id2 = $2", u1, u2).Scan(&existingID)
    if err == nil {
        return existingID, targetUserID, nil // Чат уже есть, возвращаем его ID
    }

    // 3. Если чата нет, создаем его через транзакцию
    tx, err := r.db.Begin()
    if err != nil { return "", "", err }

    var newID string
    // Создаем запись в родительской таблице
    err = tx.QueryRow("INSERT INTO conversations (type) VALUES ('private') RETURNING id").Scan(&newID)
    if err != nil {
        tx.Rollback()
        return "", "", err
    }

    // Создаем запись в таблице chats, ВРУЧНУЮ передавая ID из conversations
    _, err = tx.Exec("INSERT INTO chats (id, user_id1, user_id2, updated_at) VALUES ($1, $2, $3, NOW())", newID, u1, u2)
    if err != nil {
        tx.Rollback()
        return "", "", err
    }

    err = tx.Commit()
    return newID, targetUserID, nil
}


func (r *Repository) GetChatParticipants(chatID string) ([]string, error) {
    var u1, u2 string
    
    // Используем правильные имена колонок (user_id1, user_id2)
    err := r.db.QueryRow("SELECT user_id1, user_id2 FROM chats WHERE id = $1", chatID).Scan(&u1, &u2)
    
    if err != nil {
        fmt.Printf("ОШИБКА В GetChatParticipants: %v\n", err)
        return nil, err
    }
    
    return []string{u1, u2}, nil
}

func (c *Repository) GetUserFromID(id string) (string, error) {
    var username string
    query := "SELECT username FROM users WHERE id = $1"
    err := c.db.QueryRow(query, id).Scan(&username)
    if err != nil{
        return "", err
    }
    return username, nil
}