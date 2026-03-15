package main

import (
    "log"
    "backend/internal/protocol_stack" 
    "github.com/joho/godotenv"
)

func main() {
    err := godotenv.Load()
    if err != nil {
        log.Fatal("error of getting env")
    }
    go func() {
        log.Println("The server was started")
        if err := protocol_stack.StartUDP(); err != nil {
            log.Printf("error from UDP server: %v", err)
        }
    }()

    if err := protocol_stack.Start(); err != nil {
        log.Fatalf("error from MAIN server: %v", err)
    }
}