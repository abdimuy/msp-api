// Package main is a one-shot bootstrap tool that calls EnsureIndex against
// the configured Meilisearch instance and then exits. Used to manually verify
// the index bootstrap during development. Run with:
//
//	MEILISEARCH_URL=http://localhost:7700 go run ./cmd/meili-bootstrap
package main

import (
	"context"
	"log"
	"os"
	"time"

	clientessearchmeili "github.com/abdimuy/msp-api/internal/clientes/infra/clientessearch"
	"github.com/abdimuy/msp-api/internal/platform/config"
	platformmeili "github.com/abdimuy/msp-api/internal/platform/meilisearch"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("meili-bootstrap: %v", err)
	}
}

func run() error {
	url := os.Getenv("MEILISEARCH_URL")
	if url == "" {
		url = "http://localhost:7700"
	}
	indexName := os.Getenv("MEILISEARCH_INDEX_NAME")
	if indexName == "" {
		indexName = "clientes"
	}

	cfg := config.Meilisearch{
		URL:       url,
		APIKey:    os.Getenv("MEILISEARCH_API_KEY"),
		IndexName: indexName,
	}

	client, err := platformmeili.NewRealClient(cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	idxCfg := clientessearchmeili.DefaultIndexConfig(cfg.IndexName)
	if err := client.EnsureIndex(ctx, idxCfg); err != nil {
		return err
	}
	log.Printf("Bootstrap succeeded: index %q is ready", cfg.IndexName) //nolint:gosec // IndexName is from operator-controlled config, not user input
	return nil
}
