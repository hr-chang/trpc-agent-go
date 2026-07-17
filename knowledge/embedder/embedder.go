//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

// Package embedder provides interfaces and implementations for text embedding.
package embedder

import (
	"context"
)

// Embedder is the interface that all embedders must implement.
//
// Error Handling Strategy:
// This interface uses a dual-layer error handling approach:
//
// 1. Function-level errors (returned as `error`):
//   - System-level failures that prevent communication
//   - Examples: nil input, network issues, invalid parameters
//   - These prevent the embedding operation from completing
//
// 2. Empty embeddings (empty slice return):
//   - API-level errors or processing failures
//   - Examples: API rate limits, content filtering, model errors
//   - These are delivered as empty slices with logged warnings
//
// Usage pattern:
//
//	embedding, err := embedder.GetEmbedding(ctx, "text to embed")
//	if err != nil {
//	    // Handle system-level errors (cannot communicate)
//	    return fmt.Errorf("failed to get embedding: %w", err)
//	}
//	if len(embedding) == 0 {
//	    // Handle API-level errors (communication succeeded, but API returned error)
//	    return fmt.Errorf("received empty embedding from API")
//	}
//	// Process successful embedding...
type Embedder interface {
	// GetEmbedding generates an embedding vector for the given text.
	//
	// Returns:
	// - A slice of float64 values representing the embedding
	// - An error for system-level failures (prevents communication)
	//
	// The embedding slice may be empty for API-level errors.
	GetEmbedding(ctx context.Context, text string) ([]float64, error)

	// GetEmbeddingWithUsage generates an embedding vector for the given text
	// and returns usage information if available.
	//
	// Returns:
	// - A slice of float64 values representing the embedding
	// - Usage information as a map (may be nil if not supported)
	// - An error for system-level failures
	GetEmbeddingWithUsage(ctx context.Context, text string) ([]float64, map[string]any, error)

	// GetDimensions returns the dimensionality of the embeddings produced by this embedder.
	// Returns 0 if dimensions are not known or configurable.
	GetDimensions() int
}

// BatchEmbedder is an optional extension implemented by embedders that can
// encode multiple texts in one remote request. Callers should preserve the
// input order: embeddings[i] must correspond to texts[i].
//
// Implementations must return an error when the response count or ordering
// cannot be validated. This prevents callers from silently attaching a vector
// to the wrong document.
type BatchEmbedder interface {
	// GetEmbeddings generates one embedding per input text.
	GetEmbeddings(ctx context.Context, texts []string) ([][]float64, error)

	// GetEmbeddingsWithUsage generates one embedding per input text and returns
	// aggregate usage information for the request when available.
	GetEmbeddingsWithUsage(
		ctx context.Context,
		texts []string,
	) ([][]float64, map[string]any, error)
}
