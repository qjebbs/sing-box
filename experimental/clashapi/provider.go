package clashapi

import (
	"context"
	"net/http"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/badjson"
	"github.com/sagernet/sing-box/common/urltest"
	"github.com/sagernet/sing/common/batch"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

func proxyProviderRouter(server *Server, router adapter.Router) http.Handler {
	r := chi.NewRouter()
	r.Get("/", getProviders(server, router))

	r.Route("/{name}", func(r chi.Router) {
		r.Use(parseProviderName, findProviderByName(router))
		r.Get("/", getProvider)
		r.Put("/", updateProvider)
		r.Get("/healthcheck", healthCheckProvider(server, router))
	})
	return r
}

func getProviders(server *Server, router adapter.Router) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var providersMap badjson.JSONObject
		for _, provider := range router.Providers() {
			providersMap.Put(provider.Tag(), providerInfo(server, router, provider))
		}
		render.JSON(w, r, render.M{
			"providers": providersMap,
		})
	}
}

func getProvider(w http.ResponseWriter, r *http.Request) {
	provider := r.Context().Value(CtxKeyProvider).(adapter.Provider)
	render.JSON(w, r, provider)
	render.NoContent(w, r)
}

func providerInfo(server *Server, router adapter.Router, p adapter.Provider) *badjson.JSONObject {
	var info badjson.JSONObject
	proxies := make([]*badjson.JSONObject, 0)
	for _, detour := range p.Outbounds() {
		proxies = append(proxies, proxyInfo(server, router, detour))
	}
	info.Put("type", "Proxy")       // Proxy, Rule
	info.Put("vehicleType", "HTTP") // HTTP, File, Compatible
	info.Put("name", p.Tag())
	info.Put("proxies", proxies)
	info.Put("updatedAt", p.UpdatedAt())
	return &info
}

func updateProvider(w http.ResponseWriter, r *http.Request) {
	provider := r.Context().Value(CtxKeyProvider).(adapter.Provider)
	if err := provider.Update(); err != nil {
		render.Status(r, http.StatusServiceUnavailable)
		render.JSON(w, r, newError(err.Error()))
		return
	}
	render.NoContent(w, r)
}

func healthCheckProvider(server *Server, router adapter.Router) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		b, _ := batch.New(context.Background(), batch.WithConcurrencyNum[any](10))
		providerName := r.Context().Value(CtxKeyProviderName).(string)
		checked := false
		for _, proxy := range router.Outbounds() {
			c, ok := proxy.(adapter.OutboundCheckGroup)
			if !ok {
				continue
			}
			tag := proxy.Tag()
			if _, ok := c.Provider(providerName); ok {
				checked = true
				b.Go(tag, func() (any, error) {
					c.CheckProvider(providerName)
					return nil, nil
				})
			}
		}
		if checked {
			b.Wait()
			render.NoContent(w, r)
			return
		}

		// the provider is not used by any outbound group
		provider := r.Context().Value(CtxKeyProvider).(adapter.Provider)
		for _, proxy := range provider.Outbounds() {
			proxy := proxy
			b.Go(proxy.Tag(), func() (any, error) {
				delay, err := urltest.URLTest(r.Context(), "http://www.gstatic.com/generate_204", proxy)
				tag := proxy.Tag()
				if err != nil {
					server.urlTestHistory.StoreURLTestHistory(tag, &urltest.History{
						Time:  time.Now(),
						Delay: 0,
					})
				} else {
					server.urlTestHistory.StoreURLTestHistory(tag, &urltest.History{
						Time:  time.Now(),
						Delay: delay,
					})
				}
				return nil, nil
			})
		}
		b.Wait()
		render.NoContent(w, r)
	}
}

func parseProviderName(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := getEscapeParam(r, "name")
		ctx := context.WithValue(r.Context(), CtxKeyProviderName, name)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func findProviderByName(router adapter.Router) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			name := r.Context().Value(CtxKeyProviderName).(string)
			provider, exist := router.Provider(name)
			if !exist {
				render.Status(r, http.StatusNotFound)
				render.JSON(w, r, ErrNotFound)
				return
			}

			ctx := context.WithValue(r.Context(), CtxKeyProvider, provider)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
