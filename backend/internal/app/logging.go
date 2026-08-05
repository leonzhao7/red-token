package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"

	"red-token/internal/domain"
)

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
	wrote  bool
}

func (w *loggingResponseWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.status = status
	w.wrote = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *loggingResponseWriter) Write(data []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += int64(n)
	return n, err
}

func (w *loggingResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *loggingResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (a *App) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if requestID == "" {
			requestID = strings.TrimSpace(r.Header.Get("X-Request-Id"))
		}
		if requestID == "" {
			requestID = newRequestID()
		}

		ctx := context.WithValue(r.Context(), requestIDContextKey, requestID)
		r = r.WithContext(ctx)
		w.Header().Set("X-Request-ID", requestID)

		startedAt := time.Now()
		recorder := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}
		if strings.HasPrefix(r.URL.Path, "/v1") {
			a.logEvent(ctx, slog.LevelInfo, "client_request_started", requestAttrs(r)...)
			next.ServeHTTP(recorder, r)
			a.logEvent(ctx, slog.LevelInfo, "client_request_finished", append(requestAttrs(r),
				slog.Int("status", recorder.status),
				slog.Int64("response_bytes", recorder.bytes),
				slog.Duration("duration", time.Since(startedAt)),
			)...)
		} else {
			next.ServeHTTP(recorder, r)
		}
	})
}

func (a *App) withBackendTrace(ctx context.Context, backend domain.Backend, attempt int) context.Context {
	trace := &httptrace.ClientTrace{
		ConnectDone: func(network, addr string, err error) {
			attrs := append(backendAttemptAttrs(backend, attempt),
				slog.String("network", network),
				slog.String("addr", addr),
			)
			if err != nil {
				attrs = append(attrs, slog.String("error", err.Error()))
				a.logEvent(ctx, slog.LevelWarn, "backend_connect_done", attrs...)
				return
			}
			a.logEvent(ctx, slog.LevelInfo, "backend_connect_done", attrs...)
		},
		GotConn: func(info httptrace.GotConnInfo) {
			attrs := append(backendAttemptAttrs(backend, attempt),
				slog.Bool("reused", info.Reused),
				slog.Bool("was_idle", info.WasIdle),
				slog.Duration("idle_time", info.IdleTime),
			)
			if info.Conn != nil {
				attrs = append(attrs,
					slog.String("remote_addr", info.Conn.RemoteAddr().String()),
					slog.String("local_addr", info.Conn.LocalAddr().String()),
				)
			}
			a.logEvent(ctx, slog.LevelInfo, "backend_connection_acquired", attrs...)
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			attrs := backendAttemptAttrs(backend, attempt)
			if info.Err != nil {
				attrs = append(attrs, slog.String("error", info.Err.Error()))
				a.logEvent(ctx, slog.LevelWarn, "backend_request_written", attrs...)
				return
			}
			a.logEvent(ctx, slog.LevelInfo, "backend_request_written", attrs...)
		},
		GotFirstResponseByte: func() {
			a.logEvent(ctx, slog.LevelInfo, "backend_first_response_byte", backendAttemptAttrs(backend, attempt)...)
		},
	}
	return httptrace.WithClientTrace(ctx, trace)
}

func (a *App) logEvent(ctx context.Context, level slog.Level, message string, attrs ...slog.Attr) {
	base := []slog.Attr{slog.String("request_id", requestIDFromContext(ctx))}
	a.logger.LogAttrs(ctx, level, message, append(base, attrs...)...)
}

func requestAttrs(r *http.Request) []slog.Attr {
	return []slog.Attr{
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.String("query", r.URL.RawQuery),
		slog.String("proto", r.Proto),
		slog.String("host", r.Host),
		slog.String("remote_addr", r.RemoteAddr),
		slog.String("client_ip", clientIP(r)),
		slog.String("user_agent", r.UserAgent()),
		slog.Int64("content_length", r.ContentLength),
	}
}

func clientAttrs(client domain.ClientKey) []slog.Attr {
	return []slog.Attr{
		slog.Int64("client_id", client.ID),
		slog.String("client_name", client.Name),
		slog.String("client_token_prefix", client.TokenPrefix),
	}
}

func backendAttemptAttrs(backend domain.Backend, attempt int) []slog.Attr {
	attrs := []slog.Attr{
		slog.Int("attempt", attempt),
		slog.Int64("backend_id", backend.ID),
		slog.String("backend_name", backend.Name),
		slog.String("backend_status", backend.Status),
		slog.String("backend_protocol", domain.NormalizeBackendProtocol(backend.Protocol)),
		slog.String("backend_base_url", backend.BaseURL),
		slog.Int64("backend_proxy_id", backend.ProxyID),
	}
	if backend.Proxy != nil {
		attrs = append(attrs,
			slog.String("backend_proxy_name", backend.Proxy.Name),
			slog.String("backend_proxy_address", backend.Proxy.Address),
			slog.Bool("backend_proxy_enabled", backend.Proxy.Enabled),
		)
	} else {
		attrs = append(attrs, slog.String("backend_proxy_name", "direct"))
	}
	return attrs
}

func candidateNames(backends []domain.Backend) []string {
	names := make([]string, 0, len(backends))
	for _, backend := range backends {
		names = append(names, backend.Name)
	}
	return names
}

func clientIP(r *http.Request) string {
	if forwardedFor := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwardedFor != "" {
		parts := strings.Split(forwardedFor, ",")
		return strings.TrimSpace(parts[0])
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	return r.RemoteAddr
}

func requestIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(requestIDContextKey).(string)
	return value
}

func newRequestID() string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(raw[:])
}
