package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"red-token/internal/domain"
)

func TestOpenMigratesProxyProtocolWithSOCKS5Default(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE socks_proxies (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		address TEXT NOT NULL,
		username TEXT NOT NULL DEFAULT '',
		password TEXT NOT NULL DEFAULT '',
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("create legacy proxy schema: %v", err)
	}
	_, err = db.Exec(`INSERT INTO socks_proxies(name, address, username, password, enabled, created_at, updated_at)
		VALUES('legacy-proxy', '127.0.0.1:1080', '', '', 1, '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert legacy proxy: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	st, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	defer st.Close()
	legacy, err := st.GetSocksProxy(ctx, 1)
	if err != nil {
		t.Fatalf("load legacy proxy after migration: %v", err)
	}
	if legacy.Protocol != domain.ProxyProtocolSOCKS5 {
		t.Fatalf("migrated proxy protocol=%q, want socks5", legacy.Protocol)
	}
	created, err := st.CreateSocksProxy(ctx, domain.ProxyServer{
		Name:    "legacy-default",
		Address: "127.0.0.1:1080",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("create proxy after migration: %v", err)
	}
	if created.Protocol != domain.ProxyProtocolSOCKS5 {
		t.Fatalf("default proxy protocol=%q, want socks5", created.Protocol)
	}
	loaded, err := st.GetSocksProxy(ctx, created.ID)
	if err != nil {
		t.Fatalf("reload migrated proxy: %v", err)
	}
	if loaded.Protocol != domain.ProxyProtocolSOCKS5 {
		t.Fatalf("stored proxy protocol=%q, want socks5", loaded.Protocol)
	}
}
