package router

import (
	"net/http"

	"gateway-service/internal/clients"
	"gateway-service/internal/router/initrouter"
	"gateway-service/internal/router/middlewares"

	"github.com/redis/go-redis/v9"
)

func Setup(mux *http.ServeMux, c *clients.Clients, cache *redis.Client) {
	RegisterAuth(mux, c, cache)
	RegisterUser(mux, c, cache)
	RegisterChat(mux, c, cache)
	RegisterMessaging(mux, c)
	RegisterFiles(mux, c)
	RegisterFriends(mux, c)
	RegisterWS(mux, c, cache)
	RegisterGetVersion(mux, c)
	initrouter.RegisterInitClient(mux, c)
	mux.Handle("/registerFCMToken", middlewares.Chain(handleRegisterFCMToken(cache), middlewares.JWTMiddleware(c)))
}
