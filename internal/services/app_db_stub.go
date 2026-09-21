//go:build !postgresql

package services

import (
	"context"
	"errors"

	"codergag/internal/config"
	"codergag/internal/graph"
)

func newSQLGraphRepository(ctx context.Context, dsn string) (graph.GraphRepository, error) {
	return nil, errors.New("postgresql support not enabled; build with -tags postgresql")
}

func buildPostgresDSN(cfg config.DatabaseConfig) string {
	return ""
}