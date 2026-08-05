package handler

import (
	"net/http"
	"strings"

	"red-token/internal/domain"
	"red-token/internal/store"
)

type EventHandler struct {
	store *store.Store
}

func NewEventHandler(st *store.Store) *EventHandler {
	return &EventHandler{store: st}
}

func EnsureAuditEvents(values []domain.AuditEvent) []domain.AuditEvent {
	if values == nil {
		return []domain.AuditEvent{}
	}
	return values
}

func (a *EventHandler) HandleListEvents(w http.ResponseWriter, r *http.Request) {
	page, limit := parsePageQuery(r)
	filter := eventFilterFromRequest(r)
	total, err := a.store.CountAuditEventsFiltered(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	events, err := a.store.ListAuditEventsPageFiltered(r.Context(), filter, limit, pageOffset(page, limit))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pagedResponse(EnsureAuditEvents(events), total, page, limit))
}

func (a *EventHandler) HandleEventSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := a.store.EventSummary(r.Context(), eventFilterFromRequest(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	categories := make([]map[string]any, 0, len(summary.Categories))
	for _, item := range summary.Categories {
		categories = append(categories, map[string]any{"category": item.Name, "count": item.Count})
	}
	severities := make([]map[string]any, 0, len(summary.Severities))
	for _, item := range summary.Severities {
		severities = append(severities, map[string]any{"severity": item.Name, "count": item.Count})
	}
	actors := make([]map[string]any, 0, len(summary.Actors))
	for _, item := range summary.Actors {
		actors = append(actors, map[string]any{"actor": item.Name, "count": item.Count})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total":       summary.Total,
		"categories":  categories,
		"severities":  severities,
		"actors":      actors,
		"time_series": []any{},
	})
}

func (a *EventHandler) HandleEventDetail(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	event, err := a.store.GetAuditEvent(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"overview": map[string]any{
			"type":        event.Type,
			"message":     event.Message,
			"category":    event.Category,
			"severity":    nonEmpty(event.Severity, event.Level),
			"actor":       nonEmpty(event.Actor, "system"),
			"backend":     event.BackendName,
			"client_name": event.ClientName,
			"model":       event.Model,
			"endpoint":    event.Endpoint,
		},
		"configuration": map[string]any{},
		"metadata": map[string]any{
			"id":            event.ID,
			"created_at":    event.CreatedAt,
			"resource_type": event.ResourceType,
			"resource_id":   event.ResourceID,
		},
		"raw":      event,
		"activity": map[string]any{},
	})
}

func (a *EventHandler) HandleClearEvents(w http.ResponseWriter, r *http.Request) {
	deleted, err := a.store.ClearAuditEvents(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cleared": true,
		"deleted": deleted,
	})
}

func eventFilterFromRequest(r *http.Request) store.EventFilter {
	return store.EventFilter{
		Category: strings.TrimSpace(r.URL.Query().Get("category")),
		Severity: strings.TrimSpace(r.URL.Query().Get("severity")),
		Actor:    strings.TrimSpace(r.URL.Query().Get("actor")),
		Backend:  strings.TrimSpace(r.URL.Query().Get("backend")),
		Query:    strings.TrimSpace(r.URL.Query().Get("q")),
		DateFrom: parseTimeQuery(r.URL.Query().Get("date_from")),
		DateTo:   parseTimeQuery(r.URL.Query().Get("date_to")),
	}
}

func nonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
