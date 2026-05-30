
package clients

import (
    "os"

    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"

    authpb "postly/proto/auth"
    userpb "postly/proto/user"  
    // chatpb "postly/proto/chat"
)

type Clients struct {
    Auth authpb.AuthServiceClient
    User userpb.UserServiceClient
    // Chat chatpb.ChatServiceClient
}

func New() *Clients {
    return &Clients{
        Auth: authpb.NewAuthServiceClient(dial(os.Getenv("AUTH_SERVICE_ADDR"))),
        User: userpb.NewUserServiceClient(dial(os.Getenv("USER_SERVICE_ADDR"))),
        // Chat: chatpb.NewChatServiceClient(dial(os.Getenv("CHAT_SERVICE_ADDR"))),
    }
}

func dial(addr string) *grpc.ClientConn {
    conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {
        panic("failed to connect to " + addr + ": " + err.Error())
    }
    return conn
}
