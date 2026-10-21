package main

import (
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"postly/pkg/events"
	"postly/pkg/kafkax"
	"syscall"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

type Handler struct {}

func (h *Handler) HandleMessage(message []byte, offset kafka.Offset) error {
	e := events.Event{}
	err := json.Unmarshal(message, &e); 
	if err != nil {
		return err
	} 
	log.Printf("got message: type: %v, actor_id: %v, recipients_ids: %v, request_id: %v\n",
	 e.Type, e.ActorID, e.RecipientsIDs, e.Entity["request_id"])
	return nil
}

func main() {
	var k *kafkax.Consumer
	var err error
	kafkaServers := getenv("KAFKA_BOOTSTRAP_SERVERS", "")
	if kafkaServers != "" {
		k, err = kafkax.NewConsumer(&Handler{}, []string{kafkaServers}, "notification-events", "notification-service")
		if err != nil {
			log.Fatalf("notification-service: error creating consumer: %v", err)
		}
	} else {
		log.Fatalf("kafka consumer not exist\n")
	}
	go func() {
		k.Start()
	}()
	defer k.Stop()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit
	log.Println("shutting down notification-service...")
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
