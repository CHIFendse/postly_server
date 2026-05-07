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
	Id string `json:"id"`
	Name string `json:"name"`
	Created_at int `json:"created_at"`
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
	query := "SELECT c.id, u.username FROM chats c JOIN users u ON u.id = CASE WHEN c.user_id1 = $1 THEN c.user_id2 ELSE c.user_id1 END WHERE c.user_id1 = $1 OR c.user_id2 = $1;"
	rows, err := c.db.Query(query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	chats := make([]*Chats, 0)
	for rows.Next(){
		m := new(Chats)
		err := rows.Scan(&m.Id, &m.Name)
		if err != nil {
			return nil, err
		} 
		chats = append(chats, m)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	return chats, nil
}

func (c *Repository) AddMessage(chat_id, sender_id, text string) (string, error){
	var id string
	query := `INSERT INTO Messages (conversation_id, sender_id, text) VALUES($1, $2, $3) RETURNING id;`
	err := c.db.QueryRow(query, chat_id, sender_id, text).Scan(&id)
	if err != nil {
		fmt.Printf("err: %s", err)
		return  "", err
	}
	return id, nil
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
    _, err = tx.Exec("INSERT INTO chats (id, user_id1, user_id2) VALUES ($1, $2, $3)", newID, u1, u2)
    if err != nil {
        tx.Rollback()
        return "", "", err
    }

    err = tx.Commit()
    return newID, targetUserID, nil
}
