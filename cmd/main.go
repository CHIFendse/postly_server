package main

import (
    "log"
    "backend/internal/protocol_stack" 
    "github.com/joho/godotenv"
)

func main() {
    err := godotenv.Load()
    if err != nil {
        log.Fatal("Ошибка загрузки .env файла")
    }
    log.Println("Сервер DisMes запускается...")
    go func() {
        log.Println("Запуск UDP на :8082 (голос)...")
        if err := protocol_stack.StartUDP(); err != nil {
            log.Printf("Ошибка UDP сервера: %v", err)
        }
    }()

    log.Println("Запуск основного сервера на :8081...")
    if err := protocol_stack.Start(); err != nil {
        log.Fatalf("Критическая ошибка основного сервера: %v", err)
    }
}