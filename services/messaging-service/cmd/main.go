package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	"messaging-service/internal/db"
	"messaging-service/internal/server"
	chatpb "postly/proto/chat"
	msgpb "postly/proto/messaging"
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	chatConn, err := grpc.DialContext(ctx, os.Getenv("CHAT_SERVICE_ADDR"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		log.Fatalf("cannot connect to chat-service: %v", err)
	}
	defer chatConn.Close()

	srv := grpc.NewServer()
	msgpb.RegisterMessagingServiceServer(srv, server.New(conn, cache, chatpb.NewChatServiceClient(chatConn)))
	reflection.Register(srv)

	addr := ":" + getenv("GRPC_PORT", "50054")
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-quit
		log.Println("shutting down messaging-service...")
		srv.GracefulStop()
	}()

	log.Printf("messaging-service listening on %s", addr)
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
