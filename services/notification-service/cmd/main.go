package cmd

import (
	"postly/pkg/kafkax"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

type Handler struct {
}

func (h *Handler) HandleMessage(message []byte, offset kafka.Offset) error {

}
func main() {
	k, err := kafkax.NewConsumer(&Handler{}, )
}