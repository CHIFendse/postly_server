package middlewares

import (
	"context"
	"net/http"
	"regexp"

	"github.com/google/uuid"
)

var allowedCharsRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func isValid(s string) bool {
	return allowedCharsRegex.MatchString(s) &&  len(s) <= 64 && len(s) > 0

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
