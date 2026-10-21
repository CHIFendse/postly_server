package kafkax

import (
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

var sessionTimeout = 5000

type Handler interface {
	HandleMessage(message []byte, offset kafka.Offset) error
}

type Consumer struct {
	consumer *kafka.Consumer
	handler  Handler
	stop     atomic.Bool
}

func NewConsumer(handler Handler, address []string, topic, consumerGroup string) (*Consumer, error) {
	conf := kafka.ConfigMap{
		"bootstrap.servers":  strings.Join(address, ","),
		"group.id":           consumerGroup,
		"session.timeout.ms": sessionTimeout,
		"enable.auto.commit": false,
		"auto.offset.reset":  "earliest",
	}

	c, err := kafka.NewConsumer(&conf)
	if err != nil {
		return nil, fmt.Errorf("error of initialize consumer: %v\n", err)
	}

	if err = c.Subscribe(topic, nil); err != nil {
		return nil, fmt.Errorf("error of subcribing: %v\n", err)
	}

	return &Consumer{consumer: c, handler: handler}, nil
}

func (c *Consumer) Start() {
	if c == nil { return }
	for {
		if c.stop.Load() {
			break
		}
		msg, err := c.consumer.ReadMessage(time.Second)
		if err != nil {
			if ke, ok := err.(kafka.Error); ok && ke.Code() == kafka.ErrTimedOut {
				continue
			}
			log.Printf("kafka: %v", err)
			continue
		}
		if err = c.handler.HandleMessage(msg.Value, msg.TopicPartition.Offset); err != nil {
			log.Printf("kafka: обработка не удалась: %v", err)
			continue
		}
		c.consumer.CommitMessage(msg)

	}
}

func (c *Consumer) Stop() error {
	if c == nil { return fmt.Errorf("consumer struct empty") }
	c.stop.Store(true)
	if _, err := c.consumer.Commit(); err != nil {
		log.Printf("stop error consumer: %v\n", err)
	}

	return c.consumer.Close()
}
