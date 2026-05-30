package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"user-service/internal/db"
	"user-service/internal/server"
	userpb "postly/proto/user"
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

	srv := grpc.NewServer()
	userpb.RegisterUserServiceServer(srv, server.New(conn))
	reflection.Register(srv)

	addr := ":" + getenv("GRPC_PORT", "50052")
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-quit
		log.Println("shutting down user-service...")
		srv.GracefulStop()
	}()

	log.Printf("user-service listening on %s", addr)
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
