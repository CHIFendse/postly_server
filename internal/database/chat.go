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
	Username string `json:"username"`
	Created_at int `json:"created_at"`
}

type Messages struct {
	Id string `json:"id"`
	Text string `json:"text"`
	Chat_id string `json:"chat_id"`
	Sender_id string `json:"sender_id"`
	Created_at time.Time `json:"created_at"`
}


func (c *Repository)GetMessages(chat_id string) ([]*Messages, error){
	query := `SELECT id, text, chat_id, sender_id, created_at FROM messages WHERE chat_id = $1`
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
		err := rows.Scan(&m.Id, &m.Username)
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
	query := `INSERT INTO Messages (chat_id, sender_id, text) VALUES($1, $2, $3) RETURNING id;`
	err := c.db.QueryRow(query, chat_id, sender_id, text).Scan(&id)
	if err != nil {
		fmt.Printf("err: %s", err)
		return  "", err
	}
	return id, nil
}

func (r *Repository) CreateNewChat(userID string, targetUsername string) (string, error) {
    var targetUserID string
    
	
    err := r.db.QueryRow("SELECT id FROM users WHERE LOWER(username) = LOWER($1)", targetUsername).Scan(&targetUserID)
    if err != nil {
        if err == sql.ErrNoRows {
            return "", fmt.Errorf("пользователь %s не найден", targetUsername)
        }
        return "", err
    }

    u1, u2 := userID, targetUserID
    if u1 > u2 {
        u1, u2 = u2, u1
    }

    var chatID string
    query := `
	INSERT INTO chats (user_id1, user_id2)
	VALUES ($1, $2)
	ON CONFLICT (user_id1, user_id2) DO UPDATE 
	SET user_id1 = EXCLUDED.user_id1 
	RETURNING id;`

    err = r.db.QueryRow(query, u1, u2).Scan(&chatID)
    if err != nil {
        return "", fmt.Errorf("SQL error: %v", err)
    }

    return chatID, nil
}