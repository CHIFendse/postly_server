package middlewares

import (
	"fmt"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"golang.org/x/time/rate"
)
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

func (l *limiterCache) getLimiter(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.m[ip]; !ok {
		l.m[ip] = &object{limiter: rate.NewLimiter(rate.Limit(l.speed), l.spike)}
	}
	l.m[ip].lastMsg = time.Now()
	return l.m[ip].limiter
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


func RecoveryMonitor() {
	if v := recover(); v != nil {
		if v == http.ErrAbortHandler {
			return
		}
		stack := debug.Stack()
		fmt.Printf("GOROUTINE PANIC: %v, STACK: %s\n", v, stack)
	}
}

func RateLimitingMiddleware(speed, spike int) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		cache := InitLimiterCache(speed, spike)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			if !cache.getLimiter(ip).Allow() {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(429)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
