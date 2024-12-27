package clashapi

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/urltest"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/protocol/group"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/batch"
	F "github.com/sagernet/sing/common/format"
	"github.com/sagernet/sing/common/json/badjson"
	N "github.com/sagernet/sing/common/network"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

func proxyRouter(server *Server, router adapter.Router) http.Handler {
	r := chi.NewRouter()
	r.Get("/", getProxies(server))

	r.Route("/{name}", func(r chi.Router) {
		r.Use(parseProxyName, findProxyByName(server))
		r.Get("/", getProxy(server))
		r.Get("/delay", getProxyDelay(server))
		r.Put("/", updateProxy)
	})
	return r
}

func parseProxyName(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := getEscapeParam(r, "name")
		ctx := context.WithValue(r.Context(), CtxKeyProxyName, name)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func findProxyByName(server *Server) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			name := r.Context().Value(CtxKeyProxyName).(string)
			proxy, exist := server.outbound.Outbound(name)
			if !exist {
				render.Status(r, http.StatusNotFound)
				render.JSON(w, r, ErrNotFound)
				return
			}
			ctx := context.WithValue(r.Context(), CtxKeyProxy, proxy)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func proxyInfo(server *Server, detour adapter.Outbound) *badjson.JSONObject {
	var info badjson.JSONObject
	var clashType string
	switch detour.Type() {
	case C.TypeBlock:
		clashType = "Reject"
	default:
		clashType = C.ProxyDisplayName(detour.Type())
	}
	info.Put("type", clashType)
	info.Put("name", detour.Tag())
	info.Put("udp", common.Contains(detour.Network(), N.NetworkUDP))
	real, err := adapter.RealOutbound(detour)
	if err != nil {
		info.Put("history", []*urltest.History{})
	} else {
		delayHistory := server.urlTestHistory.LoadURLTestHistory(real.Tag())
		if delayHistory != nil {
			info.Put("history", []*urltest.History{delayHistory})
		} else {
			info.Put("history", []*urltest.History{})
		}
	}
	if group, isGroup := detour.(adapter.OutboundGroup); isGroup {
		info.Put("now", group.Now())
		info.Put("all", group.All())
	}
	return &info
}

func getProxies(server *Server) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var proxyMap badjson.JSONObject
		outbounds := common.Filter(server.outbound.Outbounds(), func(detour adapter.Outbound) bool {
			return detour.Tag() != ""
		})
		outbounds = append(outbounds, common.Map(common.Filter(server.endpoint.Endpoints(), func(detour adapter.Endpoint) bool {
			return detour.Tag() != ""
		}), func(it adapter.Endpoint) adapter.Outbound {
			return it
		})...)

		allProxies := make([]string, 0, len(outbounds))

		for _, detour := range outbounds {
			switch detour.Type() {
			case C.TypeDirect, C.TypeBlock, C.TypeDNS:
				continue
			}
			allProxies = append(allProxies, detour.Tag())
		}

		defaultTag := server.outbound.Default().Tag()

		sort.SliceStable(allProxies, func(i, j int) bool {
			return allProxies[i] == defaultTag
		})

		// fix clash dashboard
		proxyMap.Put("GLOBAL", map[string]any{
			"type":    "Fallback",
			"name":    "GLOBAL",
			"udp":     true,
			"history": []*urltest.History{},
			"all":     allProxies,
			"now":     defaultTag,
		})

		for i, detour := range outbounds {
			var tag string
			if detour.Tag() == "" {
				tag = F.ToString(i)
			} else {
				tag = detour.Tag()
			}
			proxyMap.Put(tag, proxyInfo(server, detour))
		}
		var responseMap badjson.JSONObject
		responseMap.Put("proxies", &proxyMap)
		response, err := responseMap.MarshalJSON()
		if err != nil {
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, newError(err.Error()))
			return
		}
		w.Write(response)
	}
}

func getProxy(server *Server) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		proxy := r.Context().Value(CtxKeyProxy).(adapter.Outbound)
		response, err := proxyInfo(server, proxy).MarshalJSON()
		if err != nil {
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, newError(err.Error()))
			return
		}
		w.Write(response)
	}
}

type UpdateProxyRequest struct {
	Name string `json:"name"`
}

func updateProxy(w http.ResponseWriter, r *http.Request) {
	req := UpdateProxyRequest{}
	if err := render.DecodeJSON(r.Body, &req); err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, ErrBadRequest)
		return
	}

	proxy := r.Context().Value(CtxKeyProxy).(adapter.Outbound)
	selector, ok := proxy.(*group.SelectorProvider)
	if !ok {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, newError("Must be a Selector"))
		return
	}

	if !selector.SelectOutbound(req.Name) {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, newError("Selector update error: not found"))
		return
	}

	render.NoContent(w, r)
}

func getProxyDelay(server *Server) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		proxyName := r.Context().Value(CtxKeyProxyName).(string)
		proxy := r.Context().Value(CtxKeyProxy).(adapter.Outbound)
		// yacd may request the delay of a group
		if group, isGroup := proxy.(adapter.OutboundGroup); isGroup {
			outbound, err := adapter.RealOutbound(group)
			if err != nil {
				render.Status(r, http.StatusServiceUnavailable)
				render.JSON(w, r, newError(err.Error()))
				return
			}
			proxy = outbound
			proxyName = outbound.Tag()
		}
		b, _ := batch.New(context.Background(), batch.WithConcurrencyNum[any](10))
		var (
			delay   uint16
			checked bool
		)
		for _, proxy := range server.outbound.Outbounds() {
			c, ok := proxy.(adapter.OutboundCheckGroup)
			if !ok {
				continue
			}
			if _, ok := c.Outbound(proxyName); !ok {
				continue
			}
			checked = true
			b.Go(proxyName, func() (any, error) {
				d, err := c.CheckOutbound(r.Context(), proxyName)
				if err == nil {
					// last delay from all tests from groups
					delay = d
				}
				return nil, nil
			})
		}
		if checked {
			b.Wait()
			if delay == 0 {
				render.Status(r, http.StatusServiceUnavailable)
				render.JSON(w, r, newError("An error occurred in the delay test"))
				return
			}
			render.JSON(w, r, render.M{
				"delay": delay,
			})
			return
		}

		// the proxy is not used by any outbound group
		query := r.URL.Query()
		url := query.Get("url")
		if strings.HasPrefix(url, "http://") {
			url = ""
		}
		timeout, err := strconv.ParseInt(query.Get("timeout"), 10, 16)
		if err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, ErrBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*time.Duration(timeout))
		defer cancel()

		delay, err = urltest.URLTest(ctx, url, proxy)
		defer func() {
			real, err := adapter.RealOutbound(proxy)
			if err != nil {
				server.urlTestHistory.StoreURLTestHistory(proxy.Tag(), &urltest.History{
					Time:  time.Now(),
					Delay: 0,
				})
			} else {
				server.urlTestHistory.StoreURLTestHistory(real.Tag(), &urltest.History{
					Time:  time.Now(),
					Delay: delay,
				})
			}
		}()

		if ctx.Err() != nil {
			render.Status(r, http.StatusGatewayTimeout)
			render.JSON(w, r, ErrRequestTimeout)
			return
		}

		if err != nil || delay == 0 {
			render.Status(r, http.StatusServiceUnavailable)
			render.JSON(w, r, newError("An error occurred in the delay test"))
			return
		}

		render.JSON(w, r, render.M{
			"delay": delay,
		})
	}
}
