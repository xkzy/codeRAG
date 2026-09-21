//go:build mongodb

package services

import (
	"context"

	"codergag/internal/config"
	"codergag/internal/graph"
)

func newMongoGraphRepository(ctx context.Context, dsn, projectID string) (graph.GraphRepository, error) {
	return graph.NewMongoGraphRepository(ctx, dsn, projectID)
}

func buildMongoDSN(cfg config.DatabaseConfig) string {
	if cfg.DSN != "" {
		return cfg.DSN
	}
	port := cfg.Port
	if port == 0 {
		port = 27017
	}
	auth := ""
	if cfg.User != "" {
		auth = cfg.User
		if cfg.Password != "" {
			auth += ":" + cfg.Password
		}
		auth += "@"
	}
	return "mongodb://" + auth + cfg.Host + ":" + string(rune(port)) + "/" + cfg.Database
}