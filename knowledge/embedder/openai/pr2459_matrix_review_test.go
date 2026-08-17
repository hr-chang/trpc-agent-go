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
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	openaioption "github.com/openai/openai-go/option"
)

func pr2459MatrixDecodeInputs(r *http.Request) (map[string]json.RawMessage, []string, error) {
	var body map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, nil, err
	}
	var inputs []string
	if err := json.Unmarshal(body["input"], &inputs); err != nil {
		return body, nil, err
	}
	return body, inputs, nil
}

func pr2459MatrixWriteResponse(
	w http.ResponseWriter,
	data []map[string]any,
) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   data,
		"model":  "pr2459-matrix",
		"usage": map[string]any{
			"prompt_tokens": len(data),
			"total_tokens":  len(data),
		},
	})
}

func pr2459MatrixItems(inputs []string, reverse bool) []map[string]any {
	items := make([]map[string]any, 0, len(inputs))
	appendItem := func(i int) {
		items = append(items, map[string]any{
			"object":    "embedding",
			"index":     i,
			"embedding": pr2459ReviewVectorForText(inputs[i]),
		})
	}
	if reverse {
		for i := len(inputs) - 1; i >= 0; i-- {
			appendItem(i)
		}
		return items
	}
	for i := range inputs {
		appendItem(i)
	}
	return items
}

// TestPR2459Matrix_GetEmbeddingsWireParity verifies that the new array-input
// path preserves the single-input path's model, dimensions, user, encoding,
// and per-request options across model families.
func TestPR2459Matrix_GetEmbeddingsWireParity(t *testing.T) {
	tests := []struct {
		name           string
		model          string
		explicitDims   bool
		dimensions     int
		wantDimensions bool
		wantDims       int64
	}{
		{name: "non text-embedding-3 model omits implicit dimensions", model: "bge-m3"},
		{
			name:           "non text-embedding-3 model forwards explicit dimensions",
			model:          "bge-m3",
			explicitDims:   true,
			dimensions:     2,
			wantDimensions: true,
			wantDims:       2,
		},
		{
			name:           "text-embedding-3 model keeps historical default dimensions",
			model:          ModelTextEmbedding3Small,
			wantDimensions: true,
			wantDims:       DefaultDimensions,
		},
	}

	texts := []string{"重复", "duplicate", "duplicate", "  "}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				body      map[string]json.RawMessage
				gotInputs []string
				header    string
				serveErr  error
			)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				header = r.Header.Get("X-PR2459-Matrix")
				body, gotInputs, serveErr = pr2459MatrixDecodeInputs(r)
				if serveErr != nil {
					http.Error(w, serveErr.Error(), http.StatusBadRequest)
					return
				}
				pr2459MatrixWriteResponse(w, pr2459MatrixItems(gotInputs, true))
			}))
			t.Cleanup(srv.Close)

			opts := []Option{
				WithBaseURL(srv.URL),
				WithAPIKey("matrix-key"),
				WithModel(tt.model),
				WithEncodingFormat(EncodingFormatFloat),
				WithUser("matrix-user"),
				WithRequestOptions(openaioption.WithHeader("X-PR2459-Matrix", "present")),
				WithMaxRetries(0),
			}
			if tt.explicitDims {
				opts = append(opts, WithDimensions(tt.dimensions))
			}
			emb := New(opts...)
			vectors, err := emb.GetEmbeddings(context.Background(), texts)
			if err != nil {
				t.Fatalf("GetEmbeddings() error = %v", err)
			}
			if serveErr != nil {
				t.Fatalf("server decode error = %v", serveErr)
			}
			if !slices.Equal(gotInputs, texts) {
				t.Errorf("wire inputs=%q, want %q", gotInputs, texts)
			}
			if header != "present" {
				t.Errorf("request option header=%q, want present", header)
			}

			var gotModel, gotEncoding, gotUser string
			if err := json.Unmarshal(body["model"], &gotModel); err != nil {
				t.Fatalf("decode model: %v", err)
			}
			if err := json.Unmarshal(body["encoding_format"], &gotEncoding); err != nil {
				t.Fatalf("decode encoding_format: %v", err)
			}
			if err := json.Unmarshal(body["user"], &gotUser); err != nil {
				t.Fatalf("decode user: %v", err)
			}
			if gotModel != tt.model || gotEncoding != EncodingFormatFloat || gotUser != "matrix-user" {
				t.Errorf("wire options model=%q encoding=%q user=%q",
					gotModel, gotEncoding, gotUser)
			}
			rawDims, hasDimensions := body["dimensions"]
			if hasDimensions != tt.wantDimensions {
				t.Errorf("dimensions present=%v, want %v; body=%s",
					hasDimensions, tt.wantDimensions, rawDims)
			}
			if tt.wantDimensions {
				var gotDims int64
				if err := json.Unmarshal(rawDims, &gotDims); err != nil {
					t.Fatalf("decode dimensions: %v", err)
				}
				if gotDims != tt.wantDims {
					t.Errorf("dimensions=%d, want %d", gotDims, tt.wantDims)
				}
			}
			for i, text := range texts {
				want := pr2459ReviewVectorForText(text)
				if !slices.Equal(vectors[i], want) {
					t.Errorf("vector %d=%v, want %v", i, vectors[i], want)
				}
			}
		})
	}
}

// TestPR2459Matrix_GetEmbeddingsRetriesMixedFailures verifies that HTTP and
// protocol failures consume one shared retry budget without degrading a batch
// into per-text requests.
func TestPR2459Matrix_GetEmbeddingsRetriesMixedFailures(t *testing.T) {
	texts := []string{"first", "second", "third"}
	var (
		mu       sync.Mutex
		requests [][]string
		attempts atomic.Int64
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, inputs, err := pr2459MatrixDecodeInputs(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, slices.Clone(inputs))
		mu.Unlock()
		switch attempts.Add(1) {
		case 1:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"message": "retry matrix transport failure"},
			})
		case 2:
			items := pr2459MatrixItems(inputs, false)
			delete(items[0], "index")
			pr2459MatrixWriteResponse(w, items)
		default:
			pr2459MatrixWriteResponse(w, pr2459MatrixItems(inputs, true))
		}
	}))
	t.Cleanup(srv.Close)

	emb := New(
		WithBaseURL(srv.URL),
		WithAPIKey("matrix-key"),
		WithModel("matrix-model"),
		WithDimensions(2),
		WithMaxRetries(2),
		WithRetryBackoff([]time.Duration{0}),
	)
	vectors, err := emb.GetEmbeddings(context.Background(), texts)
	if err != nil {
		t.Fatalf("GetEmbeddings() error = %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts=%d, want 3", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 3 {
		t.Fatalf("requests=%d, want 3", len(requests))
	}
	for i, request := range requests {
		if !slices.Equal(request, texts) {
			t.Errorf("request %d inputs=%q, want whole batch %q", i, request, texts)
		}
	}
	for i, text := range texts {
		if want := pr2459ReviewVectorForText(text); !slices.Equal(vectors[i], want) {
			t.Errorf("vector %d=%v, want %v", i, vectors[i], want)
		}
	}
}

// TestPR2459Matrix_GetEmbeddingsCancelsDuringBackoff covers cancellation after
// a request fails, rather than only a context cancelled before the first call.
func TestPR2459Matrix_GetEmbeddingsCancelsDuringBackoff(t *testing.T) {
	var attempts atomic.Int64
	responded := make(chan struct{})
	var respondedOnce sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "backoff cancellation probe"},
		})
		respondedOnce.Do(func() { close(responded) })
	}))
	t.Cleanup(srv.Close)

	emb := New(
		WithBaseURL(srv.URL),
		WithAPIKey("matrix-key"),
		WithMaxRetries(3),
		WithRetryBackoff([]time.Duration{time.Hour}),
	)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := emb.GetEmbeddings(ctx, []string{"first", "second"})
		result <- err
	}()

	select {
	case <-responded:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first failed response")
	}
	// Give the caller time to enter its one-hour backoff, then prove context
	// cancellation interrupts it rather than waiting or issuing another call.
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("GetEmbeddings() error=%v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("GetEmbeddings() did not stop after cancellation")
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("HTTP attempts=%d, want 1", got)
	}
}

// TestPR2459Matrix_GetEmbeddingsConcurrentRetryIsolation stresses one shared
// embedder while every caller independently fails once and retries its batch.
func TestPR2459Matrix_GetEmbeddingsConcurrentRetryIsolation(t *testing.T) {
	var (
		mu       sync.Mutex
		attempts = make(map[string]int)
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, inputs, err := pr2459MatrixDecodeInputs(r)
		if err != nil || len(inputs) == 0 {
			http.Error(w, "invalid inputs", http.StatusBadRequest)
			return
		}
		key := inputs[0]
		mu.Lock()
		attempts[key]++
		attempt := attempts[key]
		mu.Unlock()
		if attempt == 1 {
			pr2459MatrixWriteResponse(w, pr2459MatrixItems(inputs[:1], false))
			return
		}
		pr2459MatrixWriteResponse(w, pr2459MatrixItems(inputs, true))
	}))
	t.Cleanup(srv.Close)

	emb := New(
		WithBaseURL(srv.URL),
		WithAPIKey("matrix-key"),
		WithModel("matrix-model"),
		WithDimensions(2),
		WithMaxRetries(1),
		WithRetryBackoff([]time.Duration{0}),
	)
	const calls = 24
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
				errCh <- fmt.Errorf("call %d: %w", call, err)
				return
			}
			for i, text := range texts {
				want := pr2459ReviewVectorForText(text)
				if !slices.Equal(vectors[i], want) {
					errCh <- fmt.Errorf("call %d vector %d=%v, want %v", call, i, vectors[i], want)
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
	mu.Lock()
	defer mu.Unlock()
	if len(attempts) != calls {
		t.Errorf("independent retry keys=%d, want %d", len(attempts), calls)
	}
	for key, got := range attempts {
		if got != 2 {
			t.Errorf("attempts for %q=%d, want 2", key, got)
		}
	}
}

// TestPR2459Matrix_GetEmbeddingsRejectsIndexBoundaries expands the protocol
// matrix to negative, just-outside, and very large response indices.
func TestPR2459Matrix_GetEmbeddingsRejectsIndexBoundaries(t *testing.T) {
	for _, badIndex := range []int64{-1, 2, 1 << 62} {
		t.Run(fmt.Sprintf("index=%d", badIndex), func(t *testing.T) {
			srv := newPR2459ReviewEmbeddingServer(t, func([]string) []map[string]any {
				return []map[string]any{
					{"object": "embedding", "index": 0, "embedding": []float64{1}},
					{"object": "embedding", "index": badIndex, "embedding": []float64{2}},
				}
			})
			emb := New(
				WithBaseURL(srv.URL),
				WithAPIKey("matrix-key"),
				WithModel("matrix-model"),
				WithDimensions(1),
				WithMaxRetries(0),
			)
			if vectors, err := emb.GetEmbeddings(context.Background(), []string{"a", "b"}); err == nil {
				t.Fatalf("GetEmbeddings() accepted index %d and returned %v", badIndex, vectors)
			}
		})
	}
}
