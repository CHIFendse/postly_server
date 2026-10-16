package middlewares

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type AttemptsConfig struct {
	Name string
	MaxAttempts int
	FailWindow time.Duration
	BaseCooldown time.Duration
	MaxCooldown time.Duration
	StrikeTTL time.Duration
	Refund bool
	KeyField string
}

const maxKeyBody = 4 << 10


func keyPartFromBody(r *http.Request, field string) string {
	if field == "" || r.Body == nil {
		return ""
	}
	orig := r.Body
	buf, err := io.ReadAll(io.LimitReader(orig, maxKeyBody+1))
	r.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(buf), orig), orig}
	if err != nil || len(buf) > maxKeyBody {
		return ""
	}

	var m map[string]any
	if json.Unmarshal(buf, &m) != nil {
		return ""
	}
	v, _ := m[field].(string)
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}


func getRedisKeysForAttemptsMdw(name, ip, part string) []string {
	prefix := "rl:" + name + ":" + ip
	if part != "" {
		prefix += ":" + part
	}
	return []string{prefix + ":lock", prefix + ":fails", prefix + ":strikes"}
}

func AttemptsMiddleware(cache *redis.Client, cfg *AttemptsConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			keys := getRedisKeysForAttemptsMdw(cfg.Name, ip, keyPartFromBody(r, cfg.KeyField))
			ctx, cancel := context.WithTimeout(r.Context(), 300*time.Millisecond)
			defer cancel()

			res, err := luaScript.Run(ctx, cache, keys, cfg.MaxAttempts, cfg.FailWindow.Milliseconds(),
			cfg.BaseCooldown.Milliseconds(), cfg.MaxCooldown.Milliseconds(), cfg.StrikeTTL.Milliseconds()).Int64Slice()
			if err != nil {
				fmt.Printf("[ERROR] in REQUEST_ID: %v, err: %s\n", 
				r.Context().Value(RequestIDKey), err.Error())
				next.ServeHTTP(w, r)
				return
			} else if len(res) != 2 {
				fmt.Printf("[ERROR] in REQUEST_ID: %v, err: len of script result != 2\n", 
				r.Context().Value(RequestIDKey))
				next.ServeHTTP(w, r)
				return
			}
			if res[0] == 0 {
				w.Header().Set("Retry-After", strconv.Itoa(int((res[1]+999)/1000)))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(429)
				data := map[string]string{"message": "Слишком много попыток, пожалуйста, подождите, осталось: " + strconv.Itoa(int((res[1]+999)/1000)) +" секунд."}
				jsonData, _ := json.Marshal(data)
				w.Write(jsonData)
				return
			}
			next.ServeHTTP(w, r)
			sw, ok := w.(*StatusWriter)
			if !ok { return }
			if sw.status != http.StatusUnauthorized && cfg.Refund {
				ctx, cancel = context.WithTimeout(context.Background(), 300*time.Millisecond)
				defer cancel()
				if err := refundScript.Run(ctx, cache, []string{keys[1]}).Err(); err != nil {
					fmt.Printf("[ERROR] refund attempt, REQUEST_ID: %v, err: %s\n",
						r.Context().Value(RequestIDKey), err.Error())
				}
			}
		})
	}
}

var luaScript *redis.Script = redis.NewScript(`
	local lockTtl = redis.call('PTTL', KEYS[1])
	if lockTtl > 0 then
	return {0, lockTtl}
	end

	local fails = redis.call('INCR', KEYS[2])
	if fails == 1 or redis.call('PTTL', KEYS[2]) < 0 then
	redis.call('PEXPIRE', KEYS[2], ARGV[2])
	end

	if fails <= tonumber(ARGV[1]) then
	return {1, 0}
	end

	local strikes = redis.call('INCR', KEYS[3])
	redis.call('PEXPIRE', KEYS[3], ARGV[5])

	local cooldown = tonumber(ARGV[3]) * 2 ^ (strikes - 1)
	if cooldown > tonumber(ARGV[4]) then
	cooldown = tonumber(ARGV[4])
	end
	cooldown = math.floor(cooldown)

	redis.call('SET', KEYS[1], 1, 'PX', cooldown)
	redis.call('DEL', KEYS[2])
	return {0, cooldown}
`)

var refundScript *redis.Script = redis.NewScript(`
	local v = tonumber(redis.call('GET', KEYS[1]) or '0')
	if v > 0 then
		return redis.call('DECR', KEYS[1])
	end
	return 0
`)