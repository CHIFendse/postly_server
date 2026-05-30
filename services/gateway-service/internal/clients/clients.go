package clients

import (
	"context"
	"log"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	authpb    "postly/proto/auth"
	chatpb    "postly/proto/chat"
	friendspb "postly/proto/friends"
	msgpb     "postly/proto/messaging"
	userpb    "postly/proto/user"
)

type Clients struct {
	Auth      authpb.AuthServiceClient
	User      userpb.UserServiceClient
	Chat      chatpb.ChatServiceClient
	Messaging msgpb.MessagingServiceClient
	Friends   friendspb.FriendsServiceClient
}

func New() *Clients {
	return &Clients{
		Auth:      authpb.NewAuthServiceClient(dial(os.Getenv("AUTH_SERVICE_ADDR"))),
		User:      userpb.NewUserServiceClient(dial(os.Getenv("USER_SERVICE_ADDR"))),
		Chat:      chatpb.NewChatServiceClient(dial(os.Getenv("CHAT_SERVICE_ADDR"))),
		Messaging: msgpb.NewMessagingServiceClient(dial(os.Getenv("MESSAGING_SERVICE_ADDR"))),
		Friends:   friendspb.NewFriendsServiceClient(dial(os.Getenv("FRIENDS_SERVICE_ADDR"))),
	}
}

func dial(addr string) *grpc.ClientConn {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		log.Fatalf("cannot connect to %s: %v", addr, err)
	}
	log.Printf("connected to %s", addr)
	return conn
}
