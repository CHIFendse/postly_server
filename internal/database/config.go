package database

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq" // Драйвер для PostgreSQL
)

// InitDB загружает настройки и возвращает готовое соединение с базой
func InitDB() (*sql.DB, error) {
	// 1. Загружаем переменные из .env файла
	// Если файла нет, godotenv выдаст ошибку, которую мы поймаем
	err := godotenv.Load()
	if err != nil {
		return nil, fmt.Errorf("ошибка загрузки .env файла: %v", err)
	}

	// 2. Формируем строку подключения (DSN)
	// Берем данные, которые ты прописал в .env
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"),
	)

	// 3. Открываем соединение
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("ошибка открытия базы: %v", err)
	}

	// 4. Проверяем, реально ли база отвечает (Ping)
	err = db.Ping()
	if err != nil {
		return nil, fmt.Errorf("база данных недоступна: %v", err)
	}

	fmt.Println("Успешное подключение к PostgreSQL!")
	return db, nil
}