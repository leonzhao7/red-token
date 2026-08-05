package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"red-token/internal/config"
	"red-token/internal/domain"
	"red-token/internal/store"
)

type SettingHandler struct {
	store *store.Store
	cfg   *config.Config
}

func NewSettingHandler(st *store.Store, cfg *config.Config) *SettingHandler {
	return &SettingHandler{store: st, cfg: cfg}
}

func (a *SettingHandler) HandleGetConfig(w http.ResponseWriter, r *http.Request) {
	settings, err := a.store.GetAllSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response := map[string]any{
		"listen_addr":                getSettingOrDefault(settings, "listen_addr", a.cfg.ListenAddr),
		"db_path":                    getSettingOrDefault(settings, "db_path", a.cfg.DBPath),
		"log_level":                  getSettingOrDefault(settings, "log_level", a.cfg.LogLevel),
		"backend_cooldown":           getSettingOrDefault(settings, "backend_cooldown", a.cfg.BackendCooldown.String()),
		"backend_fails":              getSettingOrDefault(settings, "backend_fails", fmt.Sprintf("%d", a.cfg.BackendFails)),
		"backend_console_user_agent": getSettingOrDefault(settings, "backend_console_user_agent", a.cfg.BackendConsoleUserAgent),
		"focus_models":               getSettingOrDefault(settings, "focus_models", a.cfg.FocusModels),
		"request_timeout":            getSettingOrDefault(settings, "request_timeout", a.cfg.RequestTimeout.String()),
		"shutdown_timeout":           getSettingOrDefault(settings, "shutdown_timeout", a.cfg.ShutdownTimeout.String()),
	}

	writeJSON(w, http.StatusOK, response)
}

func (a *SettingHandler) HandleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var payload map[string]string
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Validate settings
	if logLevel, ok := payload["log_level"]; ok {
		if !isValidLogLevel(logLevel) {
			writeError(w, http.StatusBadRequest, "invalid log_level")
			return
		}
	}

	if cooldown, ok := payload["backend_cooldown"]; ok {
		if _, err := time.ParseDuration(cooldown); err != nil {
			writeError(w, http.StatusBadRequest, "invalid backend_cooldown duration")
			return
		}
	}

	if fails, ok := payload["backend_fails"]; ok {
		if _, err := strconv.Atoi(fails); err != nil {
			writeError(w, http.StatusBadRequest, "invalid backend_fails number")
			return
		}
	}

	if userAgent, ok := payload["backend_console_user_agent"]; ok {
		if !isValidUserAgent(userAgent) {
			writeError(w, http.StatusBadRequest, "invalid backend_console_user_agent")
			return
		}
	}

	if timeout, ok := payload["request_timeout"]; ok {
		if _, err := time.ParseDuration(timeout); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request_timeout duration")
			return
		}
	}

	if timeout, ok := payload["shutdown_timeout"]; ok {
		if _, err := time.ParseDuration(timeout); err != nil {
			writeError(w, http.StatusBadRequest, "invalid shutdown_timeout duration")
			return
		}
	}

	// Save to database
	if err := a.store.SetSettings(r.Context(), payload); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Apply hot-reloadable settings immediately
	if logLevel, ok := payload["log_level"]; ok {
		slog.SetLogLoggerLevel(parseLogLevel(logLevel))
		a.cfg.LogLevel = logLevel
	}

	if cooldown, ok := payload["backend_cooldown"]; ok {
		if d, err := time.ParseDuration(cooldown); err == nil {
			a.cfg.BackendCooldown = d
		}
	}

	if fails, ok := payload["backend_fails"]; ok {
		if n, err := strconv.Atoi(fails); err == nil {
			a.cfg.BackendFails = n
		}
	}

	if userAgent, ok := payload["backend_console_user_agent"]; ok {
		a.cfg.BackendConsoleUserAgent = strings.TrimSpace(userAgent)
	}

	if focusModels, ok := payload["focus_models"]; ok {
		a.cfg.FocusModels = strings.TrimSpace(focusModels)
	}

	if timeout, ok := payload["request_timeout"]; ok {
		if d, err := time.ParseDuration(timeout); err == nil {
			a.cfg.RequestTimeout = d
		}
	}

	if keys := sortedSettingKeys(payload); len(keys) > 0 {
		_ = a.store.AppendAuditEvent(r.Context(), domain.AuditEvent{
			Type:         "admin_config_update",
			Actor:        "admin",
			ResourceType: "config",
			Message:      "configuration updated: " + strings.Join(keys, ", "),
		})
	}

	a.HandleGetConfig(w, r)
}

func (a *SettingHandler) HandleReloadConfig(w http.ResponseWriter, r *http.Request) {
	settings, err := a.store.GetAllSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Reload hot-reloadable settings
	if logLevel, ok := settings["log_level"]; ok {
		slog.SetLogLoggerLevel(parseLogLevel(logLevel))
		a.cfg.LogLevel = logLevel
	}

	if cooldown, ok := settings["backend_cooldown"]; ok {
		if d, err := time.ParseDuration(cooldown); err == nil {
			a.cfg.BackendCooldown = d
		}
	}

	if fails, ok := settings["backend_fails"]; ok {
		if n, err := strconv.Atoi(fails); err == nil {
			a.cfg.BackendFails = n
		}
	}

	if userAgent, ok := settings["backend_console_user_agent"]; ok && isValidUserAgent(userAgent) {
		a.cfg.BackendConsoleUserAgent = strings.TrimSpace(userAgent)
	}

	if focusModels, ok := settings["focus_models"]; ok {
		a.cfg.FocusModels = strings.TrimSpace(focusModels)
	}

	if timeout, ok := settings["request_timeout"]; ok {
		if d, err := time.ParseDuration(timeout); err == nil {
			a.cfg.RequestTimeout = d
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "reloaded"})
}

func getSettingOrDefault(settings map[string]string, key, defaultValue string) string {
	if value, ok := settings[key]; ok && value != "" {
		return value
	}
	return defaultValue
}

func sortedSettingKeys(settings map[string]string) []string {
	keys := make([]string, 0, len(settings))
	for key := range settings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func isValidLogLevel(level string) bool {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug", "info", "warn", "warning", "error":
		return true
	default:
		return false
	}
}

func isValidUserAgent(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 512 {
		return false
	}
	return !strings.ContainsAny(value, "\r\n")
}

func parseLogLevel(value string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
