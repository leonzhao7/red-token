package handler

import (
	"net/http"
	"time"

	"red-token/internal/domain"
	"red-token/internal/proxy"
	"red-token/internal/store"
)

type ProxyHandler struct {
	store *store.Store
}

type proxyWritePayload struct {
	Name     string  `json:"name"`
	Protocol *string `json:"protocol"`
	Address  string  `json:"address"`
	Username string  `json:"username"`
	Password string  `json:"password"`
	Enabled  bool    `json:"enabled"`
}

func NewProxyHandler(st *store.Store) *ProxyHandler {
	return &ProxyHandler{store: st}
}

type proxyView struct {
	domain.SocksProxy
	BoundBackendCount int        `json:"bound_backend_count"`
	RequestCount      int        `json:"request_count"`
	TrafficBytes      int64      `json:"traffic_bytes"`
	AvgLatencyMS      float64    `json:"avg_latency_ms"`
	LastUsedAt        *time.Time `json:"last_used_at,omitempty"`
}

func (a *ProxyHandler) HandleListSocksProxies(w http.ResponseWriter, r *http.Request) {
	page, limit := parsePageQuery(r)
	total, err := a.store.CountSocksProxies(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	proxies, err := a.store.ListSocksProxiesPage(r.Context(), limit, pageOffset(page, limit))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ids := socksProxyIDs(proxies)
	bindingCounts, err := a.store.BackendBindingCountByProxyIDs(r.Context(), ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	usageSummary, err := a.store.ProxyUsageSummaryByIDs(r.Context(), ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response := make([]proxyView, 0, len(proxies))
	for _, proxy := range proxies {
		summary := usageSummary[proxy.ID]
		response = append(response, proxyView{
			SocksProxy:        proxy,
			BoundBackendCount: bindingCounts[proxy.ID],
			RequestCount:      summary.RequestCount,
			TrafficBytes:      summary.TrafficBytes,
			AvgLatencyMS:      summary.AvgLatencyMS,
			LastUsedAt:        optionalTime(summary.LastUsedAt),
		})
	}

	writeJSON(w, http.StatusOK, pagedResponse(ensureProxyViews(response), total, page, limit))
}

func (a *ProxyHandler) HandleCreateSocksProxy(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Name     string `json:"name"`
		Protocol string `json:"protocol"`
		Address  string `json:"address"`
		Username string `json:"username"`
		Password string `json:"password"`
		Enabled  bool   `json:"enabled"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	server := domain.ProxyServer{
		Name:     payload.Name,
		Protocol: domain.NormalizeProxyProtocol(payload.Protocol),
		Address:  payload.Address,
		Username: payload.Username,
		Password: payload.Password,
		Enabled:  payload.Enabled,
	}
	if err := proxy.ValidateServer(server); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	proxy, err := a.store.CreateSocksProxy(r.Context(), server)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = a.store.AppendAuditEvent(r.Context(), domain.AuditEvent{
		Level:        "warn",
		Type:         "admin_socks_proxy_create",
		Actor:        "admin",
		ResourceType: "socks_proxy",
		ResourceID:   proxy.ID,
		Message:      "proxy created: " + proxy.Name,
	})
	writeJSON(w, http.StatusCreated, proxy)
}

func (a *ProxyHandler) HandleUpdateSocksProxy(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	current, err := a.store.GetSocksProxy(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "proxy not found")
		return
	}

	var payload proxyWritePayload
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	current.Name = payload.Name
	if payload.Protocol != nil {
		current.Protocol = domain.NormalizeProxyProtocol(*payload.Protocol)
	}
	current.Address = payload.Address
	current.Username = payload.Username
	current.Password = payload.Password
	current.Enabled = payload.Enabled
	if payload.Protocol == nil && domain.NormalizeProxyProtocol(current.Protocol) == domain.ProxyProtocolTransparent {
		current.Username = ""
		current.Password = ""
	}
	if err := proxy.ValidateServer(current); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	proxy, err := a.store.UpdateSocksProxy(r.Context(), current)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, proxy)
}

func (a *ProxyHandler) HandleDeleteSocksProxy(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	proxy, err := a.store.GetSocksProxy(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "proxy not found")
		return
	}
	if err := a.store.DeleteSocksProxy(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = a.store.AppendAuditEvent(r.Context(), domain.AuditEvent{
		Level:        "warn",
		Type:         "admin_socks_proxy_delete",
		Actor:        "admin",
		ResourceType: "socks_proxy",
		ResourceID:   proxy.ID,
		Message:      "proxy deleted: " + proxy.Name,
	})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

func (a *ProxyHandler) HandleSocksProxyDetail(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	detail, err := a.store.SocksProxyDetail(r.Context(), id, 10)
	if err != nil {
		writeError(w, http.StatusNotFound, "proxy not found")
		return
	}
	writeJSON(w, http.StatusOK, resourceDetailPayload{
		Overview: []resourceDetailEntry{
			detailEntry("name", "Name", detail.Proxy.Name),
			detailEntry("enabled", "Enabled", detail.Proxy.Enabled),
			detailEntry("bound_backends", "Bound Backends", len(detail.Backends)),
		},
		Configuration: []resourceDetailEntry{
			detailEntry("protocol", "Protocol", detail.Proxy.Protocol),
			detailEntry("address", "Address", detail.Proxy.Address),
			detailEntry("username", "Username", detail.Proxy.Username),
		},
		Metadata: []resourceDetailEntry{
			detailEntry("id", "ID", detail.Proxy.ID),
			detailEntry("created_at", "Created At", detail.Proxy.CreatedAt),
			detailEntry("updated_at", "Updated At", detail.Proxy.UpdatedAt),
		},
		Raw: detail.Proxy,
		Activity: resourceDetailActivity{
			Usage:     EnsureUsageLogs(detail.Usage),
			UsageLogs: EnsureUsageLogs(detail.Usage),
			Events:    []domain.AuditEvent{},
			Backends:  detail.Backends,
		},
	})
}

func ensureSocksProxies(values []domain.SocksProxy) []domain.SocksProxy {
	if values == nil {
		return []domain.SocksProxy{}
	}
	return values
}

func ensureProxyViews(values []proxyView) []proxyView {
	if values == nil {
		return []proxyView{}
	}
	return values
}

func socksProxyIDs(values []domain.SocksProxy) []int64 {
	if len(values) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(values))
	for _, value := range values {
		ids = append(ids, value.ID)
	}
	return ids
}
