package jellyfin

import (
	"context"

	"github.com/go-chi/chi/v5"
)

func newTestRouteContext(key, value string) context.Context {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return context.WithValue(context.Background(), chi.RouteCtxKey, rctx)
}
