package router

import (
	"net/http"

	"gateway-service/internal/clients"
	"github.com/redis/go-redis/v9"
)

func Setup(mux *http.ServeMux, c *clients.Clients, cache *redis.Client) {
	RegisterAuth(mux, c)
	RegisterUser(mux, c)
	RegisterChat(mux, c, cache)
	RegisterMessaging(mux, c)
	RegisterFiles(mux, c)
	RegisterFriends(mux, c)
	RegisterWS(mux, c, cache)
	RegisterGetVersion(mux, c)

	mux.HandleFunc("/registerFCMToken", JWTMiddleware(c, handleRegisterFCMToken(cache)))
}
