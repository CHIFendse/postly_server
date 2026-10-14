package router

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/time/rate"

	"gateway-service/internal/clients"
	authpb "postly/proto/auth"
)

type Middleware func(http.Handler) http.Handler
type contextKey string

const RequestIDKey contextKey = "request_id"
const UserIDKey contextKey = "user_id"

var _ http.Flusher = (*statusWriter)(nil)
var _ http.Hijacker = (*statusWriter)(nil)

var allowedCharsRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func isValid(s string) bool {
	return allowedCharsRegex.MatchString(s) &&  len(s) <= 64 && len(s) > 0

}

type object struct {
	limiter *rate.Limiter
	lastMsg time.Time
}

type limiterCache struct {
	m map[string]*object
	mu sync.Mutex
	speed int
	spike int
	closeDaemon chan struct{}
}
func (lc *limiterCache) Stop() {
	close(lc.closeDaemon)
}

func InitLimiterCache(speed, spike int) *limiterCache{
	lc := &limiterCache{m: make(map[string]*object), 
		mu: sync.Mutex{}, speed: speed,
		spike: spike, closeDaemon: make(chan struct{}),
	}
	go func() {
		defer RecoveryMonitor()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <- ticker.C:
				lc.mu.Lock()
				for ip, obj := range lc.m {
					if time.Since(obj.lastMsg) > time.Minute {
						delete(lc.m, ip)
					}
				}
				lc.mu.Unlock()
			case <- lc.closeDaemon:
				return
			}
		}
	}()
	return lc
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	w.wroteHeader = true
	return n, err
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}

	conn, rw, err := hijacker.Hijack()
	if err == nil {
		if !w.wroteHeader {
			w.status = 101
			w.wroteHeader = true
		}
	}
	return conn, rw, err
}

func (w *statusWriter) Flush() {
	f, ok := w.ResponseWriter.(http.Flusher)
	if !ok {
		return
	}
	f.Flush()
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func Chain(next http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		next = mws[i](next)
	}
	return next
}

func CORSMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, DELETE")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func authenticate(c *clients.Clients, r *http.Request) (string, bool) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		return "", false
	}

	resp, err := c.Auth.ValidateToken(r.Context(), &authpb.ValidateTokenRequest{Token: token})
	if err != nil || !resp.Valid {
		return "", false
	}
	return resp.UserId, true
}

func JWTMiddleware(c *clients.Clients) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := authenticate(c, r)
			if !ok {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}


func RateLimitingMiddleware(speed, spike int) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		cache := InitLimiterCache(speed, spike)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			if !cache.getLimiter(ip).Allow() {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(429)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (l *limiterCache) getLimiter(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.m[ip]; !ok {
		l.m[ip] = &object{limiter: rate.NewLimiter(rate.Limit(l.speed), l.spike)}
	}
	l.m[ip].lastMsg = time.Now()
	return l.m[ip].limiter
}

func LoggingMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			logWriter := &statusWriter{ResponseWriter: w, status: 200}
			before := time.Now()
			next.ServeHTTP(logWriter, r)
			duration := time.Since(before)
			fmt.Printf("ID: %v, METHOD: %s, PATH: %s, STATUS: %v, DURATION %s, BYTES: %d\n", 
			r.Context().Value(RequestIDKey), r.Method, r.URL.Path, logWriter.status, duration.String(), logWriter.bytes)
		})
	}
}

func RecoveryMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					if v == http.ErrAbortHandler {
						panic(v)
					}
					stack := debug.Stack()
					fmt.Printf("ID: %v, PANIC: %v, STACK: %s\n", 
					r.Context().Value(RequestIDKey), v, stack)
					
					sent := false
					if sw, ok := w.(*statusWriter); ok {
						sent = sw.wroteHeader
					}

					if !sent {
						w.WriteHeader(500)
						w.Write([]byte("500 Internal Server Error: Попробуйте перезагрузить страницу или зайти позднее"))
					}
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func RecoveryMonitor() {
	if v := recover(); v != nil {
		if v == http.ErrAbortHandler {
			return
		}
		stack := debug.Stack()
		fmt.Printf("GOROUTINE PANIC: %v, STACK: %s\n", v, stack)
	}
}


func RequestIDMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := r.Header.Get("X-Request-ID")
			if !isValid(requestID) {
				requestID = uuid.NewString()
			}
			ctx := context.WithValue(r.Context(), RequestIDKey, requestID)
			w.Header().Set("X-Request-ID", requestID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}