package protocol_stack

import (
	"net/http"
	"strings"
	"encoding/json"
	"net"
	"sync"
	"golang.org/x/time/rate"
)

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
        w.Header().Set("Access-Control-Allow-Origin", "*") 
        w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
        w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
        
        if r.Method == "OPTIONS" {
            w.WriteHeader(http.StatusOK)
            return
        }
        
        next.ServeHTTP(w, r)
    })
}


// Проверка JWT токена
func JWTMiddleware(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        authHeader := r.Header.Get("Authorization")
        if authHeader == "" {
            w.WriteHeader(http.StatusUnauthorized)
            json.NewEncoder(w).Encode(map[string]string{"error": "Missing auth token"})
            return
        }

        tokenString := strings.TrimPrefix(authHeader, "Bearer ")

        isValid, err := authService.ValidateToken(tokenString)
        if err != nil || !isValid {
            w.WriteHeader(http.StatusUnauthorized)
            json.NewEncoder(w).Encode(map[string]string{"error": "Invalid or expired token"})
            return
        }
        next(w, r)
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
