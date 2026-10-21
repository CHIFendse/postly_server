module notification-service

go 1.27.1

require (
    github.com/confluentinc/confluent-kafka-go/v2 v2.16.0
	postly/pkg v0.0.0
	postly/proto v0.0.0
)

replace postly/pkg => ../../pkg
replace postly/proto => ../../pkg/proto