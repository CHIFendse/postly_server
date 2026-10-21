module friends-service

go 1.27.1

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/confluentinc/confluent-kafka-go/v2 v2.16.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260226221140-a57be14db171 // indirect
)

require (
	github.com/joho/godotenv v1.5.1
	github.com/lib/pq v1.12.3
	github.com/redis/go-redis/v9 v9.20.0
	google.golang.org/grpc v1.81.1
	google.golang.org/protobuf v1.36.11
	postly/pkg v0.0.0
	postly/proto v0.0.0
)

replace postly/pkg => ../../pkg

replace postly/proto => ../../pkg/proto
