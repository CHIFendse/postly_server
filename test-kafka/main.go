package main

import (
 "github.com/confluentinc/confluent-kafka-go/v2/kafka"
 "fmt"
 "log"
 "time"
)

func main() {
 topic := "test-topic"
 broker := "192.168.0.89:9092"

 fmt.Println("1. Подключаемся к Kafka (создаем Producer)...")
 p, err := kafka.NewProducer(&kafka.ConfigMap{
  "bootstrap.servers": broker,
  "acks":              "all",
 })
 if err != nil {
  log.Fatalf("Ошибка создания Producer: %s", err)
 }
 defer p.Close()

 fmt.Println("2. Готовим сообщение к отправке...")
 messageValue := fmt.Sprintf("Привет из Go! Время: %s", time.Now().Format(time.RFC3339))
 
 // Запускаем фоновое чтение событий ответа, чтобы librdkafka не блокировалась
 go func() {
  for e := range p.Events() {
   switch ev := e.(type) {
   case *kafka.Message:
    if ev.TopicPartition.Error != nil {
     log.Printf("Ошибка доставки сообщения: %v\n", ev.TopicPartition.Error)
    } else {
     fmt.Printf("[Producer] Сообщение успешно доставлено в partition %d!\n", ev.TopicPartition.Partition)
    }
   }
  }
 }()

 // Отправляем сообщение (канал ответа теперь nil, так как мы читаем через p.Events())
 err = p.Produce(&kafka.Message{
  TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
  Value:          []byte(messageValue),
 }, nil)

 if err != nil {
  log.Fatalf("Ошибка вызова Produce: %s", err)
 }

 fmt.Println("3. Сбрасываем буфер (Flush)...")
 p.Flush(5000) // Ждем максимум 5 секунд, пока фоновая горутина обработает отправку

 fmt.Println("4. Подключаемся как Consumer...")
 c, err := kafka.NewConsumer(&kafka.ConfigMap{
  "bootstrap.servers": broker,
  "group.id":          "go-test-group",
  "auto.offset.reset": "earliest",
 })
 if err != nil {
  log.Fatalf("Ошибка создания Consumer: %s", err)
 }
 defer c.Close()

 c.SubscribeTopics([]string{topic}, nil)

  fmt.Println("5. [Consumer] Ожидаем сообщения в цикле (нажмите Ctrl+C для выхода)...")
 
 for {
  // Опрашиваем Кафку каждую 1 секунду
  msg, err := c.ReadMessage(1 * time.Second)
  if err != nil {
   // Если это просто таймаут ожидания — продолжаем искать новые сообщения
   if kErr, ok := err.(kafka.Error); ok && kErr.Code() == kafka.ErrTimedOut {
    continue
   }
   // Если произошла реальная критическая ошибка — логируем её
   log.Printf("[Consumer] Ошибка: %v\n", err)
   break
  }

  // Успешно прочитали сообщение!
  fmt.Printf("[Consumer] Успешно прочитано: %s\n", string(msg.Value))
  break // Выходим из цикла после получения первого тестового сообщения
 }

}