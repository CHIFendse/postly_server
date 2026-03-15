package database

import (
    "database/sql"
    "errors"
    "fmt"
    "os"
    "time"
    "strings"
    "github.com/golang-jwt/jwt/v5"
    "golang.org/x/crypto/bcrypt"
)

type AuthService struct {
    db *sql.DB
}

func NewAuthService(db *sql.DB) *AuthService {
    return &AuthService{db: db}
}

type Claims struct {
    UserID string `json:"user_id"`
    jwt.RegisteredClaims
}

func (s *AuthService) Register(username, password, email, phone string) error {
    username = strings.ToLower(strings.TrimSpace(username))
    if username == "" || password == "" {
        return errors.New("Логин и пароль не могут быть пустыми")
    }
    hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), 12)
    if err != nil {
        return fmt.Errorf("Ошибка : %v", err)
    }
    query := `INSERT INTO users (username, password_hash, email, phone_number) VALUES ($1, $2, $3, $4)`
    _, err = s.db.Exec(query, username, string(hashedPassword), email, phone)
    return err
}

func (s *AuthService) Login(username, password string) (string, error) {
    username = strings.ToLower(strings.TrimSpace(username))
    var id, storedHash string
    query := `SELECT id, password_hash FROM users WHERE username = $1 OR email = $1`
    err := s.db.QueryRow(query, username).Scan(&id, &storedHash)
    if err != nil {
        return "", errors.New("Неверное имя пользователя или пароль")
    }
    err = bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password))
    if err != nil {
        return "", errors.New("Неверный пароль")
    }

    jwtSecret := os.Getenv("JWT_SECRET")
    expirationTime := time.Now().Add(72 * time.Hour)
    claims := &Claims{
        UserID: id,
        RegisteredClaims: jwt.RegisteredClaims{
            ExpiresAt: jwt.NewNumericDate(expirationTime),
            IssuedAt:  jwt.NewNumericDate(time.Now()),
        },
    }
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    return token.SignedString([]byte(jwtSecret))
}

func (s *AuthService) ValidateToken(tokenString string) (bool, error) {
    jwtSecret := os.Getenv("JWT_SECRET")
    token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
        return []byte(jwtSecret), nil
    })
    if err != nil { return false, nil }
    return token.Valid, nil
}


func (c *Repository) GetId(username string) (string, error) {
    var id string
    query := "SELECT id FROM users WHERE username = $1"
    err := c.db.QueryRow(query, username).Scan(&id)
    if err != nil{
        return "", err
    }
    return id, nil
}

func (s *AuthService) GetUserIDFromToken(tokenString string) (string, error) {
    jwtSecret := os.Getenv("JWT_SECRET")
    token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
        return []byte(jwtSecret), nil
    })

    if err != nil || !token.Valid {
        return "", errors.New("invalid token")
    }

    if claims, ok := token.Claims.(*Claims); ok {
        return claims.UserID, nil   
    }

    return "", errors.New("invalid claims")
}