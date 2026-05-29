package database

import (
	"backend/internal/cache"
	"database/sql"
	"fmt"
	"log"
	"time"
)

var (
	// msgCache: chatID → список сообщений. TTL 60 сек, инвалидируется при AddMessage.
	msgCache = cache.New[[]*Messages]("msg")
	// userCache: userID → username. TTL 10 мин.
	userCache = cache.New[string]("user")
)

type Repository struct {
    db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
    return &Repository{db: db}
}

type Chats struct {
	Id            string `json:"id"`
	Name          string `json:"name"`
    LastMsg       string `json:"last_message"`
    LastMsgSender string `json:"username"`
    UpdatedAt     int    `json:"updated_at"`
}

type Groups struct {
    Id            string    `json:"id"`
    Name          string    `json:"name"`
    CreatedAt     time.Time `json:"created_at"`
    LastMsg       string    `json:"last_message"`
    LastMsgSender string    `json:"username"`
    UpdatedAt     int       `json:"updated_at"`
}

type Messages struct {
	Id         string    `json:"id"`
	Text       string    `json:"text"`
	Chat_id    string    `json:"chat_id"`
	Sender_id  string    `json:"sender_id"`
	Created_at time.Time `json:"created_at"`
    Username   string    `json:"username"`
}

func (c *Repository) GetGroups(id string) ([]*Groups, error) {
	query := `
        SELECT g.id, g.name, g.created_at,
               COALESCE(g.last_message, ''),
               COALESCE(sender.username, ''),
               EXTRACT(EPOCH FROM COALESCE(g.updated_at, g.created_at))::INT
        FROM groups g
        JOIN group_members gm ON g.id = gm.group_id
        LEFT JOIN users sender ON sender.id = g.last_msg_sender
        WHERE gm.user_id = $1
        ORDER BY COALESCE(g.updated_at, g.created_at) DESC
    `

    rows, err := c.db.Query(query, id)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var groups []*Groups
    for rows.Next() {
        g := &Groups{}
        err := rows.Scan(&g.Id, &g.Name, &g.CreatedAt, &g.LastMsg, &g.LastMsgSender, &g.UpdatedAt)
        if err != nil {
            return nil, err
        }
        groups = append(groups, g)
    }

    if groups == nil {
        groups = []*Groups{}
    }

    return groups, nil
}

func (c *Repository) GetMessages(chat_id string) ([]*Messages, error) {
	// Кэш-хит: не идём в БД
	if cached, ok := msgCache.Get(chat_id); ok {
		return cached, nil
	}

	// JOIN с users — один запрос вместо N+1
	query := `
		SELECT m.id, m.text, m.conversation_id, m.sender_id, m.created_at,
		       COALESCE(u.username, '')
		FROM messages m
		LEFT JOIN users u ON u.id = m.sender_id
		WHERE m.conversation_id = $1
		ORDER BY m.created_at ASC`

	rows, err := c.db.Query(query, chat_id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := make([]*Messages, 0)
	for rows.Next() {
		m := new(Messages)
		if err := rows.Scan(&m.Id, &m.Text, &m.Chat_id, &m.Sender_id, &m.Created_at, &m.Username); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	msgCache.Set(chat_id, messages, 60*time.Second)
	return messages, nil
}

func (c *Repository) GetChats(id string) ([]*Chats, error) {
	query := `
        SELECT 
            c.id, 
            u.username AS chat_name, 
            COALESCE(c.last_message, ''), 
            COALESCE(sender.username, ''),
            EXTRACT(EPOCH FROM c.updated_at)::INT
        FROM chats c 
        JOIN users u ON u.id = (
            CASE 
                WHEN c.user_id1 = $1::uuid THEN c.user_id2
                ELSE c.user_id1 
            END
        )::uuid
        LEFT JOIN users sender ON sender.id = c.last_msg_sender::uuid
        WHERE c.user_id1 = $1::uuid OR c.user_id2 = $1::uuid 
        ORDER BY c.updated_at DESC;`

    rows, err := c.db.Query(query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	chats := []*Chats{}
    
    for rows.Next(){
        m := new(Chats)
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

    var newID string
    query := `INSERT INTO messages (conversation_id, sender_id, text) VALUES($1, $2, $3) RETURNING id;`
    
    err = tx.QueryRow(query, chat_id, sender_id, text).Scan(&newID)
    if err != nil {
        fmt.Println("Ошибка добавления сообщения:", err)
        return "", err
    }

    result, err := tx.Exec(`
        UPDATE chats 
        SET last_message = $1, last_msg_sender = $2, updated_at = NOW()
        WHERE id = $3`, 
        text, sender_id, chat_id)
    if err != nil {
        return "", err
    }

    rowsAffected, _ := result.RowsAffected()

    if rowsAffected == 0 {
        // SAVEPOINT изолирует UPDATE groups: если он упадёт (тип/ограничение),
        // транзакция не уходит в aborted-state и INSERT сообщения не откатывается.
        if _, spErr := tx.Exec("SAVEPOINT sp_grp"); spErr == nil {
            _, grpErr := tx.Exec(`
                UPDATE groups
                SET last_message = $1, last_msg_sender = $2, updated_at = NOW()
                WHERE id = $3`,
                text, sender_id, chat_id)
            if grpErr != nil {
                log.Printf("groups.last_message не обновлён: %v", grpErr)
                tx.Exec("ROLLBACK TO SAVEPOINT sp_grp")
            } else {
                tx.Exec("RELEASE SAVEPOINT sp_grp")
            }
        }
    }

    if err = tx.Commit(); err != nil {
        return "", err
    }

    // Инвалидируем кэш сообщений для этого чата
    msgCache.Delete(chat_id)

    return newID, nil
}

func (r *Repository) CreateNewChat(userID string, targetUsername string) (string, string, error) {
    var targetUserID string
    err := r.db.QueryRow("SELECT id FROM users WHERE LOWER(username) = LOWER($1)", targetUsername).Scan(&targetUserID)
    if err != nil {
        return "", "", err
    }

    u1, u2 := userID, targetUserID
    if u1 > u2 { u1, u2 = u2, u1 }

    var existingID string
    err = r.db.QueryRow("SELECT id FROM chats WHERE user_id1 = $1 AND user_id2 = $2", u1, u2).Scan(&existingID)
    if err == nil {
        return existingID, targetUserID, nil
    }

    tx, err := r.db.Begin()
    if err != nil { return "", "", err }

    var newID string
    err = tx.QueryRow("INSERT INTO conversations (type) VALUES ('private') RETURNING id").Scan(&newID)
    if err != nil {
        tx.Rollback()
        return "", "", err
    }

    _, err = tx.Exec("INSERT INTO chats (id, user_id1, user_id2, updated_at) VALUES ($1, $2, $3, NOW())", newID, u1, u2)
    if err != nil {
        tx.Rollback()
        return "", "", err
    }

    err = tx.Commit()
    return newID, targetUserID, nil
}

func (r *Repository) CreateNewGroup(name string, adminID string, isPrivate bool, members []string) (string, error) {
    log.Printf("CreateNewGroup: name=%s, adminID=%s, members=%v", name, adminID, members)
    tx, err := r.db.Begin()
    if err != nil {
        return "", err
    }
    defer tx.Rollback()

    var newID string
    err = tx.QueryRow("INSERT INTO conversations (type) VALUES ('group') RETURNING id").Scan(&newID)
    if err != nil {
        return "", err
    }

    _, err = tx.Exec("INSERT INTO groups (id, name, admin_id, is_private) VALUES ($1, $2, $3, $4)", newID, name, adminID, isPrivate)
    if err != nil {
        return "", err
    }

    // Добавляем админа
    _, err = tx.Exec("INSERT INTO group_members (group_id, user_id, role) VALUES ($1, $2, 'admin')", newID, adminID)
    if err != nil {
        return "", err
    }

    // Добавляем участников по username
    for _, username := range members {
        var userID string
        err := tx.QueryRow("SELECT id FROM users WHERE LOWER(username) = LOWER($1)", username).Scan(&userID)
        if err != nil {
            log.Printf("User not found by username: %s", username)
            continue
        }
        if userID == adminID {
            continue
        }
        _, err = tx.Exec("INSERT INTO group_members (group_id, user_id, role) VALUES ($1, $2, 'member')", newID, userID)
        if err != nil {
            log.Printf("Failed to add member %s: %v", username, err)
        }
    }

    return newID, tx.Commit()
}

func (r *Repository) GetChatParticipants(chatID string) ([]string, error) {
    var u1, u2 string
    
    err := r.db.QueryRow("SELECT user_id1, user_id2 FROM chats WHERE id = $1", chatID).Scan(&u1, &u2)
    
    if err != nil {
        return nil, err
    }
    
    return []string{u1, u2}, nil
}

func (c *Repository) GetUserFromID(id string) (string, error) {
	if username, ok := userCache.Get(id); ok {
		return username, nil
	}
	var username string
	if err := c.db.QueryRow("SELECT username FROM users WHERE id = $1", id).Scan(&username); err != nil {
		return "", err
	}
	userCache.Set(id, username, 10*time.Minute)
	return username, nil
}

// DeleteMessage удаляет сообщение (только отправитель может удалить своё).
func (r *Repository) DeleteMessage(messageID, senderID string) (string, error) {
	var chatID string
	err := r.db.QueryRow(
		"DELETE FROM messages WHERE id=$1 AND sender_id=$2 RETURNING conversation_id",
		messageID, senderID,
	).Scan(&chatID)
	if err != nil {
		return "", fmt.Errorf("сообщение не найдено или нет прав")
	}
	msgCache.Delete(chatID)
	return chatID, nil
}

// ClearChat удаляет все сообщения в чате.
func (r *Repository) ClearChat(chatID, userID string) error {
	// Проверяем что пользователь — участник чата или группы
	var count int
	r.db.QueryRow(
		`SELECT COUNT(*) FROM chats WHERE id=$1 AND (user_id1=$2 OR user_id2=$2)`,
		chatID, userID,
	).Scan(&count)
	if count == 0 {
		r.db.QueryRow(
			`SELECT COUNT(*) FROM group_members WHERE group_id=$1 AND user_id=$2`,
			chatID, userID,
		).Scan(&count)
	}
	if count == 0 {
		return fmt.Errorf("нет доступа к чату")
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM messages WHERE conversation_id=$1", chatID); err != nil {
		return err
	}
	tx.Exec("UPDATE chats SET last_message=NULL, last_msg_sender=NULL WHERE id=$1", chatID)
	tx.Exec("UPDATE groups SET last_message=NULL, last_msg_sender=NULL WHERE id=$1", chatID)
	msgCache.Delete(chatID)
	return tx.Commit()
}

// DeleteChat удаляет чат и все его сообщения.
func (r *Repository) DeleteChat(chatID, userID string) error {
	// Личный чат
	res, err := r.db.Exec(
		"DELETE FROM chats WHERE id=$1 AND (user_id1=$2 OR user_id2=$2)",
		chatID, userID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		r.db.Exec("DELETE FROM messages WHERE conversation_id=$1", chatID)
		r.db.Exec("DELETE FROM conversations WHERE id=$1", chatID)
		msgCache.Delete(chatID)
		return nil
	}
	// Группа — только admin может удалить
	res, err = r.db.Exec(
		"DELETE FROM groups WHERE id=$1 AND admin_id=$2",
		chatID, userID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("нет прав на удаление чата")
	}
	r.db.Exec("DELETE FROM messages WHERE conversation_id=$1", chatID)
	r.db.Exec("DELETE FROM conversations WHERE id=$1", chatID)
	msgCache.Delete(chatID)
	return nil
}

func (r *Repository) GetGroupParticipants(groupID string) ([]string, error) {
    rows, err := r.db.Query("SELECT user_id FROM group_members WHERE group_id = $1", groupID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var participants []string
    for rows.Next() {
        var userID string
        if err := rows.Scan(&userID); err != nil {
            return nil, err
        }
        participants = append(participants, userID)
    }
    
    if len(participants) == 0 {
        return nil, fmt.Errorf("no participants for group %s", groupID)
    }
    
    return participants, nil
}