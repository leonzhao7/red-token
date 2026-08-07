package handler

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"red-token/internal/domain"
	"red-token/internal/store"
)

type ClientKeyHandler struct {
	store *store.Store
}

func NewClientKeyHandler(st *store.Store) *ClientKeyHandler {
	return &ClientKeyHandler{store: st}
}

type clientKeyView struct {
	domain.ClientKey
	MaskedToken string     `json:"masked_token"`
	UsageCount  int64      `json:"usage_count"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	TokenInput  int64      `json:"token_input"`
	TokenOutput int64      `json:"token_output"`
	ReqSuccess  int64      `json:"req_success"`
	ReqFail     int64      `json:"req_fail"`
}

func (a *ClientKeyHandler) HandleListClientKeys(w http.ResponseWriter, r *http.Request) {
	page, limit := parsePageQuery(r)
	total, err := a.store.CountClientKeys(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	clients, err := a.store.ListClientKeysPage(r.Context(), limit, pageOffset(page, limit))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	usageSummary, err := a.store.ClientKeyUsageSummaryByIDs(r.Context(), clientKeyIDs(clients))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response := make([]clientKeyView, 0, len(clients))
	for _, client := range clients {
		summary := usageSummary[client.ID]
		response = append(response, clientKeyView{
			ClientKey:   client,
			MaskedToken: maskToken(client.Token),
			UsageCount:  summary.UsageCount,
			LastUsedAt:  optionalTime(summary.LastUsedAt),
			TokenInput:  summary.InputTokensTotal,
			TokenOutput: summary.OutputTokensTotal,
			ReqSuccess:  summary.RequestSuccesses,
			ReqFail:     summary.RequestFailures,
		})
	}
	writeJSON(w, http.StatusOK, pagedResponse(ensureClientKeyViews(response), total, page, limit))
}

func (a *ClientKeyHandler) HandleCreateClientKey(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Name          string `json:"name"`
		Token         string `json:"token"`
		AllowedModels string `json:"allowed_models"`
		Enabled       bool   `json:"enabled"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	token := strings.TrimSpace(payload.Token)
	if token == "" {
		generated, err := generateToken()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "generate token failed")
			return
		}
		token = generated
	}

	client, err := a.store.CreateClientKey(r.Context(), domain.ClientKey{
		Name:          payload.Name,
		TokenHash:     store.HashToken(token),
		Token:         token,
		TokenPrefix:   tokenPrefix(token),
		AllowedModels: payload.AllowedModels,
		Enabled:       payload.Enabled,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = a.store.AppendAuditEvent(r.Context(), domain.AuditEvent{
		Level:        "warn",
		Type:         "admin_client_create",
		Actor:        "admin",
		ResourceType: "client_key",
		ResourceID:   client.ID,
		Message:      "client key created: " + client.Name,
		ClientName:   client.Name,
	})
	writeJSON(w, http.StatusCreated, map[string]any{
		"client":       client,
		"issued_token": token,
	})
}

func (a *ClientKeyHandler) HandleUpdateClientKey(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	client, err := a.store.GetClientKey(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "client key not found")
		return
	}

	var payload struct {
		Name          string `json:"name"`
		Token         string `json:"token"`
		AllowedModels string `json:"allowed_models"`
		Enabled       bool   `json:"enabled"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	client.Name = payload.Name
	client.AllowedModels = payload.AllowedModels
	client.Enabled = payload.Enabled

	updated, err := a.store.UpdateClientKey(r.Context(), client)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"client":       updated,
		"issued_token": "",
	})
}

func (a *ClientKeyHandler) HandleDeleteClientKey(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	client, err := a.store.GetClientKey(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "client key not found")
		return
	}
	if err := a.store.DeleteClientKey(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = a.store.AppendAuditEvent(r.Context(), domain.AuditEvent{
		Level:        "warn",
		Type:         "admin_client_delete",
		Actor:        "admin",
		ResourceType: "client_key",
		ResourceID:   client.ID,
		Message:      "client key deleted: " + client.Name,
		ClientName:   client.Name,
	})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

func (a *ClientKeyHandler) HandleClientKeyDetail(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	detail, err := a.store.ClientKeyDetail(r.Context(), id, 10)
	if err != nil {
		writeError(w, http.StatusNotFound, "client key not found")
		return
	}
	usageSummary, err := a.store.ClientKeyUsageSummaryByIDs(r.Context(), []int64{id})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	summary := usageSummary[id]
	writeJSON(w, http.StatusOK, resourceDetailPayload{
		Overview: []resourceDetailEntry{
			detailEntry("name", "Name", detail.Client.Name),
			detailEntry("enabled", "Enabled", detail.Client.Enabled),
			detailEntry("token_prefix", "Token Prefix", detail.Client.TokenPrefix),
			detailEntry("usage_count", "Usage Count", summary.UsageCount),
			detailEntry("request_success_count", "Request Success Count", summary.RequestSuccesses),
			detailEntry("request_failure_count", "Request Failure Count", summary.RequestFailures),
			detailEntry("input_tokens", "Input Tokens", summary.InputTokensTotal),
			detailEntry("output_tokens", "Output Tokens", summary.OutputTokensTotal),
			detailEntry("last_used_at", "Last Used At", optionalTime(summary.LastUsedAt)),
		},
		Configuration: []resourceDetailEntry{
			detailEntry("token", "Token", detail.Client.Token),
		},
		Metadata: []resourceDetailEntry{
			detailEntry("id", "ID", detail.Client.ID),
			detailEntry("created_at", "Created At", detail.Client.CreatedAt),
			detailEntry("updated_at", "Updated At", detail.Client.UpdatedAt),
		},
		Raw: detail.Client,
		Activity: resourceDetailActivity{
			Usage:     EnsureUsageLogs(detail.Usage),
			UsageLogs: EnsureUsageLogs(detail.Usage),
			Events:    EnsureAuditEvents(detail.Events),
			Backends:  []domain.Backend{},
		},
	})
}

func generateToken() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "tg-" + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func tokenPrefix(token string) string {
	token = strings.TrimSpace(token)
	if len(token) <= 8 {
		return token
	}
	return token[:8]
}

func maskToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if len(token) <= 4 {
		return token[:1] + "..."
	}
	if len(token) <= 8 {
		return token[:2] + "..." + token[len(token)-1:]
	}
	suffixLen := 4
	if len(token) < 12 {
		suffixLen = 2
	}
	if suffixLen >= len(token)-8 {
		suffixLen = len(token) - 9
		if suffixLen < 1 {
			suffixLen = 1
		}
	}
	return token[:8] + "..." + token[len(token)-suffixLen:]
}

func ensureClientKeys(values []domain.ClientKey) []domain.ClientKey {
	if values == nil {
		return []domain.ClientKey{}
	}
	return values
}

func ensureClientKeyViews(values []clientKeyView) []clientKeyView {
	if values == nil {
		return []clientKeyView{}
	}
	return values
}

func clientKeyIDs(values []domain.ClientKey) []int64 {
	if len(values) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(values))
	for _, value := range values {
		ids = append(ids, value.ID)
	}
	return ids
}

func optionalTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value.UTC()
	return &copy
}
