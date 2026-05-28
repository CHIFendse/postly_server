package protocol_stack

import (
	"net/http"
	"strings"
	"encoding/json"
	"net"
	"sync"
	"golang.org/x/time/rate"
    "context"
    "github.com/golang-jwt/jwt/v5"
    "errors"
)

func parseToken(tokenString string) (*Claims, error) {
    token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
        return getJwtKey(), nil
    })

    if err != nil {
        return nil, err
    }

    if claims, ok := token.Claims.(*Claims); ok && token.Valid {
        return claims, nil
    }

    return nil, errors.New("невалидный токен")
}

var (
    udpLimiter   = make(map[string]*rate.Limiter)
    udpLimiterMu sync.Mutex
)

type IPRateLimiter struct {
    ips map[string]*rate.Limiter
    mu  *sync.RWMutex
    r   rate.Limit
    b   int
}

func NewIPRateLimiter(r rate.Limit, b int) *IPRateLimiter {
    return &IPRateLimiter{
        ips: make(map[string]*rate.Limiter),
        mu:  &sync.RWMutex{},
        r:   r,
        b:   b,
    }
}

func (i *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
    i.mu.Lock()
    defer i.mu.Unlock()

    limiter, exists := i.ips[ip]
    if !exists {
        // r - сколько запросов в секунду, b - максимальный "всплеск" (запас)
        limiter = rate.NewLimiter(i.r, i.b)
        i.ips[ip] = limiter
    }

    return limiter
}

// CORS
func enableCORS(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Устанавливаем заголовки
        w.Header().Set("Access-Control-Allow-Origin", "*") 
        w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
        // Важно добавить Accept и X-Requested-With
        w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept, X-Requested-With")
        
        // Если это предварительный запрос (Preflight)
        if r.Method == "OPTIONS" {
            // Лучше возвращать 204 No Content или 200 OK без тела
            w.WriteHeader(http.StatusNoContent)
            return
        }
        
        next.ServeHTTP(w, r)
    })
}


type contextKey string
const UserIDKey contextKey = "userIDKey"

// Проверка JWT токена
func JWTMiddleware(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        tokenString := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
        tokenString = strings.TrimSpace(tokenString)
        if tokenString == "" {
            tokenString = r.URL.Query().Get("token")
        }
        if tokenString == "" {
            w.WriteHeader(http.StatusUnauthorized)
            json.NewEncoder(w).Encode(map[string]string{"error": "Missing auth token"})
            return
        }

        claims, err := parseToken(tokenString) 
        if err != nil {
            w.WriteHeader(http.StatusUnauthorized)
            json.NewEncoder(w).Encode(map[string]string{"error": "Invalid or expired token"})
            return
        }

        ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
        
        // 3. Передаем запрос дальше с новым контекстом
        next(w, r.WithContext(ctx))
    }
}


// Ограничение отправки запросов в HTTP
func limitMiddleware(limiter *IPRateLimiter, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        ip, _, err := net.SplitHostPort(r.RemoteAddr)
        if err != nil {
            http.Error(w, "Internal Server Error", http.StatusInternalServerError)
            return
        }

        if !limiter.GetLimiter(ip).Allow() {
            w.WriteHeader(http.StatusTooManyRequests)
            json.NewEncoder(w).Encode(map[string]string{"error": "Too many requests. Slow down!"})
            return
        }

        next.ServeHTTP(w, r)
    })
}

// Ограничение отправки пакетов в UDP
func getUDPLimiter(ip string) *rate.Limiter {
    udpLimiterMu.Lock()
    defer udpLimiterMu.Unlock()

    if l, exists := udpLimiter[ip]; exists {
        return l
    }
    l := rate.NewLimiter(100, 200)
    udpLimiter[ip] = l
    return l
}
