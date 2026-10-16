package middlewares

import (
	"fmt"
	"net/http"
	"runtime/debug"
)
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
					if sw, ok := w.(*StatusWriter); ok {
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