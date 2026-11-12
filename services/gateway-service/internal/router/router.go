package router

import (
	"net/http"

	"gateway-service/internal/clients"
	"gateway-service/internal/router/initrouter"
	"github.com/redis/go-redis/v9"
)

func Setup(mux *http.ServeMux, c *clients.Clients, cache *redis.Client) {
	RegisterAuth(mux, c, cache)
	RegisterUser(mux, c, cache)
	RegisterChat(mux, c, cache)
	RegisterFCMToken(mux, c, cache)
	RegisterMessaging(mux, c, cache)
	RegisterFiles(mux, c)
	RegisterFriends(mux, c)
	RegisterWS(mux, c, cache)
	RegisterGetVersion(mux, c)
	initrouter.RegisterInitClient(mux, c)
}
