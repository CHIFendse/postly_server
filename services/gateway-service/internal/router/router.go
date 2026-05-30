package router

import (
	"net/http"

	"gateway-service/internal/clients"
)

func Setup(mux *http.ServeMux, c *clients.Clients) {
	RegisterAuth(mux, c)
	RegisterUser(mux, c)
	// RegisterChat(mux, c)
	// RegisterMessaging(mux, c)
	// RegisterFriends(mux, c)
}
