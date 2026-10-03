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

	s3pb "postly/proto/s3"
	"s3-service/internal"
)

func main() {

	if err := godotenv.Load(); err != nil {
		log.Println("Предупреждение: файл .env не найден, используются системные переменные")
	}

	s3Storage, err := internal.NewS3Client()
	if err != nil {
		log.Fatalf("Ошибка инициализации S3: %v", err)
	}
	log.Println("S3 хранилище Selectel успешно подключено")

	srv := grpc.NewServer()

	serverHandler := internal.NewServer(s3Storage)
	s3pb.RegisterFileServiceServer(srv, serverHandler)

	reflection.Register(srv)

	port := os.Getenv("GRPC_PORT")
	if port == "" {
		port = "50056"
	}
	addr := ":" + port

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Не удалось открыть порт %s: %v", addr, err)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-quit
		log.Println("Остановка gRPC сервера S3...")
		srv.GracefulStop()
	}()

	log.Printf("Сервер S3-service запущен на порту %s", addr)
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("Ошибка запуска сервера: %v", err)
	}
}
