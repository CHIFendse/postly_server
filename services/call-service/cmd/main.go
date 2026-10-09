package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"

	"call-service/internal/wsfu"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatal("cannot load .env")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rdb := redis.NewClient(&redis.Options{
		Addr:     getenv("REDIS_ADDR", "localhost:6379"),
		Password: getenv("REDIS_PASSWORD", ""),
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("redis: %v", err)
	}

	sfu := wsfu.New(ctx, rdb)
	log.Println("call-service started")
	sfu.Run()
	sfu.Close()
	log.Println("call-service stopped")
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
