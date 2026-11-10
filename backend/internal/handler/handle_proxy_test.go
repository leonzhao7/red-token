package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"red-token/internal/domain"
)

func TestProxyServerCRUDAndBackendBinding(t *testing.T) {
	st := openWorkflowHandlerStore(t)
	handler := NewProxyHandler(st)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/api/proxies", handler.HandleCreateSocksProxy)
	mux.HandleFunc("PUT /admin/api/proxies/{id}", handler.HandleUpdateSocksProxy)

	request := httptest.NewRequest(http.MethodPost, "/admin/api/proxies", strings.NewReader(`{"name":"transparent","protocol":"transparent","address":"127.0.0.1:443","enabled":true}`))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create proxy status=%d body=%s", response.Code, response.Body.String())
	}
	var created domain.ProxyServer
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode proxy response: %v", err)
	}
	if created.Protocol != domain.ProxyProtocolTransparent {
		t.Fatalf("created protocol=%q, want transparent", created.Protocol)
	}

	backend, err := st.CreateBackend(context.Background(), domain.Backend{
		Name:     "transparent-backend",
		BaseURL:  "https://upstream.example",
		Protocol: domain.BackendProtocolOpenAI,
		ProxyID:  created.ID,
		Status:   domain.BackendStatusNormal,
		APIKeys:  []domain.BackendAPIKey{{APIKey: "key"}},
	})
	if err != nil {
		t.Fatalf("create bound backend: %v", err)
	}
	if backend.Proxy == nil || backend.Proxy.Protocol != domain.ProxyProtocolTransparent {
		t.Fatalf("backend proxy after create = %+v", backend.Proxy)
	}
	loaded, err := st.GetBackend(context.Background(), backend.ID)
	if err != nil {
		t.Fatalf("reload bound backend: %v", err)
	}
	if loaded.Proxy == nil || loaded.Proxy.Protocol != domain.ProxyProtocolTransparent {
		t.Fatalf("backend proxy after reload = %+v", loaded.Proxy)
	}

	update := httptest.NewRequest(http.MethodPut, "/admin/api/proxies/"+strings.TrimSpace(jsonNumber(created.ID)), strings.NewReader(`{"name":"transparent","protocol":"socks5","address":"127.0.0.1:1080","enabled":true}`))
	updatedResponse := httptest.NewRecorder()
	mux.ServeHTTP(updatedResponse, update)
	if updatedResponse.Code != http.StatusOK {
		t.Fatalf("update proxy status=%d body=%s", updatedResponse.Code, updatedResponse.Body.String())
	}
	var updated domain.ProxyServer
	if err := json.Unmarshal(updatedResponse.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated proxy: %v", err)
	}
	if updated.Protocol != domain.ProxyProtocolSOCKS5 {
		t.Fatalf("updated protocol=%q, want socks5", updated.Protocol)
	}
}

func jsonNumber(value int64) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
