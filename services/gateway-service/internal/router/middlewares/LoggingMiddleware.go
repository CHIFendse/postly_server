package middlewares

import (
	"time"
	"net/http"
	"fmt"

)

func LoggingMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			logWriter := &StatusWriter{ResponseWriter: w, status: 200}
			before := time.Now()
			next.ServeHTTP(logWriter, r)
			duration := time.Since(before)
			fmt.Printf("FROM: %v, ID: %v, METHOD: %s, PATH: %s, STATUS: %v, DURATION %s, BYTES: %d\n", 
			r.RemoteAddr, r.Context().Value(RequestIDKey), r.Method, r.URL.Path, logWriter.status, duration.String(), logWriter.bytes)
		})
	}
}
