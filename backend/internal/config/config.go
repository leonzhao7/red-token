package config

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"red-token/internal/store"
)

const DefaultBackendConsoleUserAgent = "Red-Token/1.0"

// DefaultSchedulerUserAffinityTTL is how long a user keeps a backend reserved
// after their last request for that model.
const DefaultSchedulerUserAffinityTTL = 10 * time.Minute

type Config struct {
	ListenAddr              string
	FrontendListenAddr      string
	DBPath                  string
	LogLevel                string
	BackendCooldown         time.Duration
	BackendFails            int
	BackendConsoleUserAgent string
	ChromeCDPEndpoint       string
	FocusModels             string
	ConnectTimeout          time.Duration
	RequestTimeout          time.Duration
	ShutdownTimeout         time.Duration
	HealthCheckInterval     time.Duration
	HealthCheckBackends     string
	// SchedulerUserIsolation spreads users, identified by client IP and
	// User-Agent, across distinct backends for the same model.
	SchedulerUserIsolation   bool
	SchedulerUserAffinityTTL time.Duration
}

func Load() Config {
	return Config{
		ListenAddr:               getenv("RT_LISTEN_ADDR", ":4000"),
		FrontendListenAddr:       getenv("RT_FRONTEND_LISTEN_ADDR", ":6174"),
		DBPath:                   getenv("RT_DB_PATH", "./red-token.db"),
		LogLevel:                 getenv("RT_LOG_LEVEL", "info"),
		BackendCooldown:          getDuration("RT_BACKEND_COOLDOWN", 20*time.Minute),
		BackendFails:             getInt("RT_BACKEND_FAILS", 3),
		BackendConsoleUserAgent:  getenv("RT_BACKEND_CONSOLE_USER_AGENT", DefaultBackendConsoleUserAgent),
		ChromeCDPEndpoint:        getenv("RT_CHROME_CDP_URL", "http://127.0.0.1:9222"),
		FocusModels:              getenv("RT_FOCUS_MODELS", ""),
		ConnectTimeout:           getDuration("RT_CONNECT_TIMEOUT", 10*time.Second),
		RequestTimeout:           getDuration("RT_REQUEST_TIMEOUT", 120*time.Second),
		ShutdownTimeout:          getDuration("RT_SHUTDOWN_TIMEOUT", 30*time.Second),
		HealthCheckInterval:      getDuration("RT_HEALTH_CHECK_INTERVAL", 5*time.Minute),
		HealthCheckBackends:      getenv("RT_HEALTH_CHECK_BACKENDS", ""),
		SchedulerUserIsolation:   getBool("RT_SCHEDULER_USER_ISOLATION", true),
		SchedulerUserAffinityTTL: getDuration("RT_SCHEDULER_USER_AFFINITY_TTL", DefaultSchedulerUserAffinityTTL),
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
	if timeout, ok := settings["connect_timeout"]; ok {
		if d, err := time.ParseDuration(timeout); err == nil && d > 0 {
			cfg.ConnectTimeout = d
		}
	}
	if timeout, ok := settings["request_timeout"]; ok {
		if d, err := time.ParseDuration(timeout); err == nil {
			cfg.RequestTimeout = d
		}
	}
	if cdpAddress, ok := settings["cdp_address"]; ok {
		cfg.ChromeCDPEndpoint = cdpAddress
	}
	if interval, ok := settings["health_check_interval"]; ok {
		if d, err := time.ParseDuration(interval); err == nil {
			cfg.HealthCheckInterval = d
		}
	}
	if backends, ok := settings["health_check_backends"]; ok {
		cfg.HealthCheckBackends = backends
	}
	if isolation, ok := settings["scheduler_user_isolation"]; ok {
		if enabled, err := strconv.ParseBool(strings.TrimSpace(isolation)); err == nil {
			cfg.SchedulerUserIsolation = enabled
		}
	}
	if ttl, ok := settings["scheduler_user_affinity_ttl"]; ok {
		if d, err := time.ParseDuration(ttl); err == nil && d > 0 {
			cfg.SchedulerUserAffinityTTL = d
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

func getBool(key string, fallback bool) bool {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseBool(strings.TrimSpace(value)); err == nil {
			return parsed
		}
	}
	return fallback
}
