package database

import (
	"database/sql"
	"fmt"
	"time"
)

// MigrateFriends создаёт таблицы friend_requests и friends если их нет.
func MigrateFriends(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS friend_requests (
			id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			sender_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			receiver_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			status      VARCHAR(20) DEFAULT 'pending' CHECK (status IN ('pending','accepted','declined')),
			created_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			CONSTRAINT uq_friend_request UNIQUE (sender_id, receiver_id)
		);
		CREATE TABLE IF NOT EXISTS friends (
			id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			user_id1   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			user_id2   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			CONSTRAINT uq_friendship UNIQUE (user_id1, user_id2)
		);
		CREATE INDEX IF NOT EXISTS idx_friend_requests_receiver ON friend_requests(receiver_id) WHERE status = 'pending';
		CREATE INDEX IF NOT EXISTS idx_friends_user1 ON friends(user_id1);
		CREATE INDEX IF NOT EXISTS idx_friends_user2 ON friends(user_id2);
	`)
	return err
}

// FriendRequest — входящая заявка в друзья.
type FriendRequest struct {
	ID        string    `json:"id"`
	SenderID  string    `json:"sender_id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

// Friend — запись о друге пользователя.
type Friend struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// SendFriendRequest отправляет заявку в друзья.
// Возвращает (requestID, receiverID, error).
func (r *Repository) SendFriendRequest(senderID, targetUsername string) (string, string, error) {
	var receiverID string
	err := r.db.QueryRow(
		"SELECT id FROM users WHERE LOWER(username) = LOWER($1)", targetUsername,
	).Scan(&receiverID)
	if err != nil {
		return "", "", fmt.Errorf("пользователь не найден")
	}
	if senderID == receiverID {
		return "", "", fmt.Errorf("нельзя добавить себя в друзья")
	}

	// Уже друзья?
	u1, u2 := senderID, receiverID
	if u1 > u2 {
		u1, u2 = u2, u1
	}
	var fid string
	if r.db.QueryRow("SELECT id FROM friends WHERE user_id1=$1 AND user_id2=$2", u1, u2).Scan(&fid) == nil {
		return "", "", fmt.Errorf("вы уже друзья")
	}

	// Заявка уже существует?
	var existingID string
	err = r.db.QueryRow(
		"SELECT id FROM friend_requests WHERE sender_id=$1 AND receiver_id=$2 AND status='pending'",
		senderID, receiverID,
	).Scan(&existingID)
	if err == nil {
		return existingID, receiverID, nil
	}

	// Создаём заявку
	var reqID string
	err = r.db.QueryRow(
		"INSERT INTO friend_requests (sender_id, receiver_id) VALUES ($1,$2) ON CONFLICT (sender_id,receiver_id) DO UPDATE SET status='pending' RETURNING id",
		senderID, receiverID,
	).Scan(&reqID)
	if err != nil {
		return "", "", err
	}
	return reqID, receiverID, nil
}

// GetFriendRequests возвращает входящие ожидающие заявки для пользователя.
func (r *Repository) GetFriendRequests(userID string) ([]*FriendRequest, error) {
	rows, err := r.db.Query(`
		SELECT fr.id, fr.sender_id, u.username, fr.created_at
		FROM friend_requests fr
		JOIN users u ON u.id = fr.sender_id
		WHERE fr.receiver_id = $1 AND fr.status = 'pending'
		ORDER BY fr.created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var requests []*FriendRequest
	for rows.Next() {
		req := &FriendRequest{}
		if err := rows.Scan(&req.ID, &req.SenderID, &req.Username, &req.CreatedAt); err != nil {
			return nil, err
		}
		requests = append(requests, req)
	}
	if requests == nil {
		requests = []*FriendRequest{}
	}
	return requests, rows.Err()
}

// AcceptFriendRequest принимает заявку: ставит статус accepted и добавляет запись в friends.
func (r *Repository) AcceptFriendRequest(userID, requestID string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Проверяем, что заявка адресована именно этому пользователю
	var senderID string
	err = tx.QueryRow(
		"UPDATE friend_requests SET status='accepted' WHERE id=$1 AND receiver_id=$2 AND status='pending' RETURNING sender_id",
		requestID, userID,
	).Scan(&senderID)
	if err != nil {
		return fmt.Errorf("заявка не найдена или уже обработана")
	}

	// Вставляем дружбу (user_id1 < user_id2 для уникальности)
	u1, u2 := senderID, userID
	if u1 > u2 {
		u1, u2 = u2, u1
	}
	_, err = tx.Exec(
		"INSERT INTO friends (user_id1, user_id2) VALUES ($1,$2) ON CONFLICT DO NOTHING",
		u1, u2,
	)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// DeclineFriendRequest отклоняет (удаляет) заявку.
func (r *Repository) DeclineFriendRequest(userID, requestID string) error {
	res, err := r.db.Exec(
		"UPDATE friend_requests SET status='declined' WHERE id=$1 AND receiver_id=$2 AND status='pending'",
		requestID, userID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("заявка не найдена")
	}
	return nil
}

// GetFriends возвращает список друзей пользователя.
func (r *Repository) GetFriends(userID string) ([]*Friend, error) {
	rows, err := r.db.Query(`
		SELECT u.id, u.username
		FROM friends f
		JOIN users u ON u.id = CASE WHEN f.user_id1 = $1 THEN f.user_id2 ELSE f.user_id1 END
		WHERE f.user_id1 = $1 OR f.user_id2 = $1
		ORDER BY u.username
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var friends []*Friend
	for rows.Next() {
		fr := &Friend{}
		if err := rows.Scan(&fr.ID, &fr.Username); err != nil {
			return nil, err
		}
		friends = append(friends, fr)
	}
	if friends == nil {
		friends = []*Friend{}
	}
	return friends, rows.Err()
}
