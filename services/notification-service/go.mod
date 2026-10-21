module notification-service

go 1.27.1

require (
	github.com/confluentinc/confluent-kafka-go/v2 v2.16.0
	postly/pkg v0.0.0
)

require github.com/google/uuid v1.6.0 // indirect

replace postly/pkg => ../../pkg

