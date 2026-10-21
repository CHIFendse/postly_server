package db

import (
	"errors"
	"fmt"
	"strings"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

var unknownType = errors.New("unknown type")

type Producer struct {
	producer *kafka.Producer
}

func NewProducer(address []string) (*Producer, error) {
	conf := &kafka.ConfigMap{
		"bootstrap.servers": strings.Join(address, ","),
	}
	p, err := kafka.NewProducer(conf)
	if err != nil {
		return nil, fmt.Errorf("error in create Procuder: %v\n", err)
	}
	return &Producer{producer: p}, nil
}

func (p *Producer) Produce(message, topic, chatID string) error {
	kafkaMsg := &kafka.Message{
		TopicPartition: kafka.TopicPartition{
			Topic:     &topic,
			Partition: kafka.PartitionAny,
		},
		Value: []byte(message),
		Key:   []byte(chatID),
	}

	kafkaChan := make(chan kafka.Event)
	if err := p.producer.Produce(kafkaMsg, kafkaChan); err != nil {
		return fmt.Errorf("error of producing message: %v\n", err)
	}

	e := <-kafkaChan
	switch et := e.(type) {
	case *kafka.Message:
		return nil
	case kafka.Error:
		return fmt.Errorf("kafka error: %v\n", et)
	default:
		return unknownType
	}

}
