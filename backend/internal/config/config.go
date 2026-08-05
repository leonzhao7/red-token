package config

import (
	"context"
	"os"
	"strconv"
	"time"

	"red-token/internal/store"
)

const DefaultBackendConsoleUserAgent = "Red-Token/1.0"

type Config struct {
	ListenAddr              string
	DBPath                  string
	LogLevel                string
	BackendCooldown         time.Duration
	BackendFails            int
	BackendConsoleUserAgent string
	FocusModels             string
	RequestTimeout          time.Duration
	ShutdownTimeout         time.Duration
}

func Load() Config {
	return Config{
		ListenAddr:              getenv("RT_LISTEN_ADDR", ":4000"),
		DBPath:                  getenv("RT_DB_PATH", "./red-token.db"),
		LogLevel:                getenv("RT_LOG_LEVEL", "info"),
		BackendCooldown:         getDuration("RT_BACKEND_COOLDOWN", 20*time.Minute),
		BackendFails:            getInt("RT_BACKEND_FAILS", 3),
		BackendConsoleUserAgent: getenv("RT_BACKEND_CONSOLE_USER_AGENT", DefaultBackendConsoleUserAgent),
		FocusModels:             getenv("RT_FOCUS_MODELS", ""),
		RequestTimeout:          getDuration("RT_REQUEST_TIMEOUT", 120*time.Second),
		ShutdownTimeout:         getDuration("RT_SHUTDOWN_TIMEOUT", 30*time.Second),
	}
}

func LoadDatabase(ctx context.Context, st *store.Store) (Config, error) {
	cfg := Load()
	settings, err := st.GetAllSettings(ctx)
	if err != nil {
		return cfg, err
	}
	if log_level, ok := settings["log_level"]; ok {
		cfg.LogLevel = log_level
	}
	if cooldown, ok := settings["backend_cooldown"]; ok {
		if d, err := time.ParseDuration(cooldown); err == nil {
			cfg.BackendCooldown = d
		}
	}
	if fails, ok := settings["backend_fails"]; ok {
		if n, err := strconv.Atoi(fails); err == nil {
			cfg.BackendFails = n
		}
	}
	if userAgent, ok := settings["backend_console_user_agent"]; ok {
		cfg.BackendConsoleUserAgent = userAgent
	}
	if focusModels, ok := settings["focus_models"]; ok {
		cfg.FocusModels = focusModels
	}
	if timeout, ok := settings["request_timeout"]; ok {
		if d, err := time.ParseDuration(timeout); err == nil {
			cfg.RequestTimeout = d
		}
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
		if seconds, err := strconv.Atoi(value); err == nil {
			return time.Duration(seconds) * time.Second
		}
	}
	return fallback
}

func getInt(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return fallback
}
