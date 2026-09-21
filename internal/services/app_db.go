//go:build postgresql

package services

import (
	"context"

	"codergag/internal/config"
	"codergag/internal/graph"
)

func newSQLGraphRepository(ctx context.Context, dsn string) (graph.GraphRepository, error) {
	return graph.NewSQLGraphRepository(ctx, dsn)
}

func buildPostgresDSN(cfg config.DatabaseConfig) string {
	if cfg.DSN != "" {
		return cfg.DSN
	}
	password := cfg.Password
	if password != "" {
		password = ":" + password + "@"
	}
	port := cfg.Port
	if port == 0 {
		port = 5432
	}
	return "postgres://" + cfg.User + password + "@" + cfg.Host + ":" + string(rune(port)) + "/" + cfg.Database + "?sslmode=disable"
}