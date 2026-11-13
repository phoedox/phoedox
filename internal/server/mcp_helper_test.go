package server

import (
	"net/http"

	"github.com/phoedox/phoedox/internal/auth"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/mcp"
)

func mcpHandlerForTest(svc *core.Service, au *auth.Auth) http.Handler {
	return mcp.Handler(svc, au, "test", nil)
}
