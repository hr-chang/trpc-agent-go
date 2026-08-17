//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

//go:build pr2459review

package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	sdkopenai "github.com/openai/openai-go"
)

var pr2459ReviewEmbeddingSink [][]float64

func newPR2459ReviewEmbeddingServer(
	t *testing.T,
	data func(inputs []string) []map[string]any,
) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/embeddings") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var request struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   data(request.Input),
			"model":  "pr2459-review",
			"usage": map[string]any{
				"prompt_tokens": len(request.Input),
				"total_tokens":  len(request.Input),
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestPR2459Review_GetEmbeddingsRejectsNullishIndex exercises the public HTTP
// path rather than the response helper directly. Both an omitted index and a
// JSON null leave openai.Embedding.Index at its zero value; neither response
// establishes that the first vector belongs to input zero.
func TestPR2459Review_GetEmbeddingsRejectsNullishIndex(t *testing.T) {
	tests := []struct {
		name      string
		setToNull bool
	}{
		{name: "omitted"},
		{name: "null", setToNull: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newPR2459ReviewEmbeddingServer(t, func([]string) []map[string]any {
				first := map[string]any{
					"object":    "embedding",
					"embedding": []float64{10},
				}
				if tt.setToNull {
					first["index"] = nil
				}
				return []map[string]any{
					first,
					{"object": "embedding", "index": 1, "embedding": []float64{20}},
				}
			})
			emb := New(
				WithBaseURL(srv.URL),
				WithAPIKey("review-key"),
				WithModel("pr2459-review"),
				WithDimensions(1),
				WithMaxRetries(0),
			)

			if vectors, err := emb.GetEmbeddings(context.Background(), []string{"first", "second"}); err == nil {
				t.Fatalf("GetEmbeddings() accepted %s index and returned %v", tt.name, vectors)
			}
		})
	}
}

func pr2459ReviewVectorForText(text string) []float64 {
	sum := 0
	for _, r := range text {
		sum += int(r)
	}
	return []float64{float64(len([]rune(text))), float64(sum)}
}

// TestPR2459Review_GetEmbeddingsConcurrentOrder is a positive stress probe for
// the built-in implementation. One shared Embedder receives concurrent batch
// calls while the server returns every response in reverse order; each caller
// must still receive vectors in its own input order.
func TestPR2459Review_GetEmbeddingsConcurrentOrder(t *testing.T) {
	srv := newPR2459ReviewEmbeddingServer(t, func(inputs []string) []map[string]any {
		items := make([]map[string]any, 0, len(inputs))
		for i := len(inputs) - 1; i >= 0; i-- {
			items = append(items, map[string]any{
				"object":    "embedding",
				"index":     i,
				"embedding": pr2459ReviewVectorForText(inputs[i]),
			})
		}
		return items
	})
	emb := New(
		WithBaseURL(srv.URL),
		WithAPIKey("review-key"),
		WithModel("pr2459-review"),
		WithDimensions(2),
		WithMaxRetries(0),
	)

	const calls = 32
	errCh := make(chan error, calls)
	var wg sync.WaitGroup
	for call := 0; call < calls; call++ {
		call := call
		wg.Add(1)
		go func() {
			defer wg.Done()
			texts := []string{
				fmt.Sprintf("call-%02d-first", call),
				fmt.Sprintf("call-%02d-second", call),
				fmt.Sprintf("call-%02d-third", call),
			}
			vectors, err := emb.GetEmbeddings(context.Background(), texts)
			if err != nil {
				errCh <- fmt.Errorf("call %d: GetEmbeddings: %w", call, err)
				return
			}
			if len(vectors) != len(texts) {
				errCh <- fmt.Errorf("call %d: vectors = %d, want %d", call, len(vectors), len(texts))
				return
			}
			for i, text := range texts {
				want := pr2459ReviewVectorForText(text)
				if !slices.Equal(vectors[i], want) {
					errCh <- fmt.Errorf("call %d vector %d = %v, want %v", call, i, vectors[i], want)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

// BenchmarkPR2459Review_ResponseMapping isolates the successful response
// mapping cost from HTTP latency. The current batch path maps once to validate
// the retry attempt and then maps the same response again before returning it.
func BenchmarkPR2459Review_ResponseMapping(b *testing.B) {
	const inputs = 100
	data := make([]map[string]any, inputs)
	for i := range data {
		data[i] = map[string]any{
			"object":    "embedding",
			"index":     i,
			"embedding": []float64{float64(i)},
		}
	}
	payload, err := json.Marshal(map[string]any{
		"object": "list",
		"data":   data,
		"model":  "pr2459-review",
	})
	if err != nil {
		b.Fatal(err)
	}
	var response sdkopenai.CreateEmbeddingResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		b.Fatal(err)
	}

	b.Run("single_mapping", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			vectors, err := embeddingsFromResponse(&response, inputs)
			if err != nil {
				b.Fatal(err)
			}
			pr2459ReviewEmbeddingSink = vectors
		}
	})

	b.Run("current_success_path_two_mappings", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := embeddingsFromResponse(&response, inputs); err != nil {
				b.Fatal(err)
			}
			vectors, err := embeddingsFromResponse(&response, inputs)
			if err != nil {
				b.Fatal(err)
			}
			pr2459ReviewEmbeddingSink = vectors
		}
	})
}
