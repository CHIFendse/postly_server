module gateway-service

go 1.26.2

require (
	github.com/confluentinc/confluent-kafka-go/v2 v2.16.0
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.3
	github.com/joho/godotenv v1.5.1
	github.com/redis/go-redis/v9 v9.20.0
	golang.org/x/oauth2 v0.36.0
	golang.org/x/time v0.16.0
	google.golang.org/grpc v1.81.1
	postly/proto v0.0.0
)

replace postly/proto => ../../pkg/proto

require (
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260226221140-a57be14db171 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)
