package router

import (
	"net/http"

	"github.com/redis/go-redis/v9"
	"gateway-service/internal/clients"
)

func Setup(mux *http.ServeMux, c *clients.Clients, cache *redis.Client) {
	RegisterAuth(mux, c)
	RegisterUser(mux, c)
	RegisterChat(mux, c, cache)
	RegisterMessaging(mux, c)
	RegisterFriends(mux, c)
	RegisterWS(mux, c, cache)
}
