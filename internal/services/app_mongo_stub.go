//go:build !mongodb

package services

import (
	"context"
	"errors"

	"codergag/internal/config"
	"codergag/internal/graph"
)

func newMongoGraphRepository(ctx context.Context, dsn, projectID string) (graph.GraphRepository, error) {
	return nil, errors.New("mongodb support not enabled; build with -tags mongodb")
}

func buildMongoDSN(cfg config.DatabaseConfig) string {
	return ""
}