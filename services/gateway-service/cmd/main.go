package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"

	"gateway-service/internal/clients"
	"gateway-service/internal/router"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatal("cannot load .env")
	}

	c := clients.New()

	cache := redis.NewClient(&redis.Options{
		Addr:     getenv("REDIS_HOST", "localhost") + ":" + getenv("REDIS_PORT", "6379"),
		Password: os.Getenv("REDIS_PASSWORD"),
	})

	mux := http.NewServeMux()
	router.Setup(mux, c, cache)

	addr := ":" + getenv("HTTP_PORT", "8081")
	srv := &http.Server{Addr: addr, Handler: router.CORS(mux)}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-quit
		log.Println("shutting down gateway...")
		srv.Close()
	}()

	cert := os.Getenv("TLS_CERT")
	key := os.Getenv("TLS_KEY")

	if cert != "" && key != "" {
		log.Printf("gateway listening on %s (TLS)", addr)
		if err := srv.ListenAndServeTLS(cert, key); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve tls: %v", err)
		}
	} else {
		log.Printf("gateway listening on %s (no TLS)", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
