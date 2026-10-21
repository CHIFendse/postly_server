package kafkax

import (
	"encoding/json"
	"fmt"
	"log"
	"postly/pkg/events"

	k "github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

type Producer struct {
	producer *k.Producer
	topic string
}

func NewProducer(address string, topic string) (*Producer, error) {
	conf := &k.ConfigMap{
		"bootstrap.servers": address,
		"acks":               "all",
		"enable.idempotence": true,
		"message.timeout.ms": 10000,
	}
	p, err := k.NewProducer(conf)
	if err != nil {
		return nil, fmt.Errorf("error in create Producer: %w", err)
	}
	go func() {
		for e := range p.Events() {
			if m, ok := e.(*k.Message); ok && m.TopicPartition.Error != nil {
				log.Printf("kafka: delivery failed: %v\n", m.TopicPartition.Error)
			}
		}
	}()
	return &Producer{producer: p, topic: topic}, nil
}

func (p *Producer) Produce(message []byte, topic, key string) error {
	kafkaMsg := &k.Message{
		TopicPartition: k.TopicPartition{
			Topic:     &topic,
			Partition: k.PartitionAny,
		},
		Value: message,
		Key:   []byte(key),
	}

	if err := p.producer.Produce(kafkaMsg, nil); err != nil {
		return fmt.Errorf("error of producing message: %w", err)
	}
	return nil
}

func (p *Producer) Stop() {
	if p == nil { return }
	p.producer.Flush(5000)
	p.producer.Close()
}

func (p *Producer) PublishEvent (
	e *events.Event) error {
	if p == nil { return nil }
	if e == nil { return fmt.Errorf("nil Event") }
	dataJson, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	var key = e.Entity["request_id"]
	if e.Type == events.MessageCreated {
		if v, ok := e.Entity["chat_id"]; ok {
			key = v
		}
	} 

	if err := p.Produce(dataJson, p.topic, key); err != nil {
		return fmt.Errorf("produce error: %w", err)
	}
	return nil
}