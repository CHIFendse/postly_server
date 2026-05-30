
package clients

import (
    "context"
    "log"
    "os"
    "time"

    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"

    authpb "postly/proto/auth"
    userpb "postly/proto/user"
    chatpb "postly/proto/chat"
)

type Clients struct {
    Auth authpb.AuthServiceClient
    User userpb.UserServiceClient
    Chat chatpb.ChatServiceClient
}

func New() *Clients {
    return &Clients{
        Auth: authpb.NewAuthServiceClient(dial(os.Getenv("AUTH_SERVICE_ADDR"))),
        User: userpb.NewUserServiceClient(dial(os.Getenv("USER_SERVICE_ADDR"))),
        Chat: chatpb.NewChatServiceClient(dial(os.Getenv("CHAT_SERVICE_ADDR"))),
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
