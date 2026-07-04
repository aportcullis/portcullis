package server

import "net/http"

// Mount attaches an HTTP route — typically a Connect RPC handler — to the server.
type Mount struct {
	Pattern string
	Handler http.Handler
}
