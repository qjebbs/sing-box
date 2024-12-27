package clashapi

import (
	"context"
	"net/http"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/urltest"
	"github.com/sagernet/sing/common/batch"
	"github.com/sagernet/sing/common/json/badjson"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

func proxyProviderRouter(server *Server) http.Handler {
	r := chi.NewRouter()
	r.Get("/", getProviders(server))

	r.Route("/{name}", func(r chi.Router) {
		r.Use(parseProviderName, findProviderByName(server))
		r.Get("/", getProvider)
		r.Put("/", updateProvider)
		r.Get("/healthcheck", checkProvider(server))
	})
	return r
}

func getProviders(server *Server) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var responseMap, providersMap badjson.JSONObject
		for _, provider := range server.provider.Providers() {
			providersMap.Put(provider.Tag(), providerInfo(server, provider))
		}
		responseMap.Put("providers", &providersMap)
		response, err := responseMap.MarshalJSON()
		if err != nil {
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, newError(err.Error()))
			return
		}
		w.Write(response)
	}
}

func getProvider(w http.ResponseWriter, r *http.Request) {
	provider := r.Context().Value(CtxKeyProvider).(adapter.Provider)
	render.JSON(w, r, provider)
	render.NoContent(w, r)
}

func providerInfo(server *Server, p adapter.Provider) *badjson.JSONObject {
	var info badjson.JSONObject
	proxies := make([]*badjson.JSONObject, 0)
	for _, detour := range p.Outbounds() {
		proxies = append(proxies, proxyInfo(server, detour))
	}
	info.Put("type", "Proxy")       // Proxy, Rule
	info.Put("vehicleType", "HTTP") // HTTP, File, Compatible
	info.Put("name", p.Tag())
	info.Put("proxies", proxies)
	info.Put("updatedAt", p.UpdatedAt())
	if p, ok := p.(adapter.ProviderInfoer); ok {
		info.Put("subscriptionInfo", p.Info())
	}
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

func checkProvider(server *Server) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		b, _ := batch.New(context.Background(), batch.WithConcurrencyNum[map[string]uint16](10))
		providerName := r.Context().Value(CtxKeyProviderName).(string)
		providerChecked := false
		for _, proxy := range server.outbound.Outbounds() {
			c, ok := proxy.(adapter.OutboundCheckGroup)
			if !ok {
				continue
			}
			tag := proxy.Tag()
			if _, ok := c.Provider(providerName); ok {
				providerChecked = true
				b.Go(tag, func() (map[string]uint16, error) {
					return c.CheckProvider(r.Context(), providerName)
				})
			}
		}

		checked := make(map[string]bool)
		if providerChecked {
			result, err := b.WaitAndGetResult()
			if err != nil {
				render.Status(r, http.StatusServiceUnavailable)
				render.JSON(w, r, newError(err.Error()))
				return
			}
			for _, r := range result {
				for k := range r.Value {
					checked[k] = true
				}
			}
		}

		// some outbounds may not be used by any group
		provider := r.Context().Value(CtxKeyProvider).(adapter.Provider)
		b2, _ := batch.New(context.Background(), batch.WithConcurrencyNum[any](10))
		for _, proxy := range provider.Outbounds() {
			real, err := adapter.RealOutbound(proxy)
			if err != nil {
				render.Status(r, http.StatusServiceUnavailable)
				render.JSON(w, r, newError(err.Error()))
				return
			}
			if _, ok := checked[real.Tag()]; ok {
				continue
			}
			b2.Go(real.Tag(), func() (any, error) {
				delay, err := urltest.URLTest(r.Context(), "", proxy)
				tag := real.Tag()
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
		b2.Wait()
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

func findProviderByName(server *Server) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			name := r.Context().Value(CtxKeyProviderName).(string)
			provider, exist := server.provider.Provider(name)
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
