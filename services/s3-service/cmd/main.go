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

	// ЗАМЕНИТЕ "s3-service" на имя вашего модуля из go.mod, если оно отличается
	"s3-service/internal"
	s3pb "postly/proto/s3"
)

func main() {
	// Загружаем переменные из файла .env, который лежит рядом в папке cmd
	if err := godotenv.Load(); err != nil {
		log.Println("Предупреждение: файл .env не найден, используются системные переменные")
	}

	// 1. Инициализируем S3 клиент
	s3Storage, err := internal.NewS3Client()
	if err != nil {
		log.Fatalf("Ошибка инициализации S3: %v", err)
	}
	log.Println("S3 хранилище Selectel успешно подключено")

	// 2. Создаем gRPC сервер
	srv := grpc.NewServer()


	// 3. Создаем наш серверный хендлер и регистрируем его в gRPC
	serverHandler := internal.NewServer(s3Storage)
	s3pb.RegisterFileServiceServer(srv, serverHandler)
	
	// Включаем рефлексию для отладки
	reflection.Register(srv)

	// Настраиваем порт
	port := os.Getenv("GRPC_PORT")
	if port == "" {
		port = "50056"
	}
	addr := ":" + port
	
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Не удалось открыть порт %s: %v", addr, err)
	}

	// Graceful Shutdown (безопасная остановка)
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
