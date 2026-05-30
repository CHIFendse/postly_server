package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"friends-service/internal/db"
	"friends-service/internal/server"
	friendspb "postly/proto/friends"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatal("cannot load .env")
	}

	conn, err := db.Init()
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer conn.Close()

	cache := redis.NewClient(&redis.Options{
		Addr:     getenv("REDIS_HOST", "localhost") + ":" + getenv("REDIS_PORT", "6379"),
		Password: os.Getenv("REDIS_PASSWORD"),
	})

	srv := grpc.NewServer()
	friendspb.RegisterFriendsServiceServer(srv, server.New(conn, cache))
	reflection.Register(srv)

	addr := ":" + getenv("GRPC_PORT", "50055")
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-quit
		log.Println("shutting down friends-service...")
		srv.GracefulStop()
	}()

	log.Printf("friends-service listening on %s", addr)
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
