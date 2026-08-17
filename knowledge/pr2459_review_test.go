//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

//go:build pr2459review

package knowledge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

var errPR2459ReviewStoreFailure = errors.New("pr2459 review store failure")

// BenchmarkPR2459Review_ValidateEmbeddingBatch measures the deliberate full
// vector scan separately from provider and vector-store latency.
func BenchmarkPR2459Review_ValidateEmbeddingBatch(b *testing.B) {
	const (
		inputs     = 100
		dimensions = 1536
	)
	embeddings := make([][]float64, inputs)
	for i := range embeddings {
		embeddings[i] = make([]float64, dimensions)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := validateEmbeddingBatch(embeddings, inputs); err != nil {
			b.Fatal(err)
		}
	}
}

type pr2459ReviewSource struct {
	name string
	docs []*document.Document
}

func newPR2459ReviewSource(name string, count int) *pr2459ReviewSource {
	docs := make([]*document.Document, count)
	for i := range docs {
		docs[i] = &document.Document{
			ID:      fmt.Sprintf("review-doc-%d", i),
			Name:    fmt.Sprintf("Review document %d", i),
			Content: fmt.Sprintf("review content %d", i),
			Metadata: map[string]any{
				source.MetaSourceName: name,
				source.MetaURI:        fmt.Sprintf("review://%d", i),
				source.MetaChunkIndex: i,
			},
		}
	}
	return &pr2459ReviewSource{name: name, docs: docs}
}

func (s *pr2459ReviewSource) ReadDocuments(context.Context) ([]*document.Document, error) {
	return s.docs, nil
}

func (s *pr2459ReviewSource) Name() string { return s.name }

func (*pr2459ReviewSource) Type() string { return "pr2459-review" }

func (*pr2459ReviewSource) GetMetadata() map[string]any { return nil }

type pr2459ReviewBatchEmbedder struct {
	singleCalls atomic.Int64
	batchCalls  atomic.Int64
}

func (e *pr2459ReviewBatchEmbedder) GetEmbedding(
	context.Context,
	string,
) ([]float64, error) {
	e.singleCalls.Add(1)
	return []float64{1, 2, 3}, nil
}

func (e *pr2459ReviewBatchEmbedder) GetEmbeddingWithUsage(
	ctx context.Context,
	text string,
) ([]float64, map[string]any, error) {
	vector, err := e.GetEmbedding(ctx, text)
	return vector, nil, err
}

func (*pr2459ReviewBatchEmbedder) GetDimensions() int { return 3 }

func (e *pr2459ReviewBatchEmbedder) GetEmbeddings(
	ctx context.Context,
	texts []string,
) ([][]float64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.batchCalls.Add(1)
	vectors := make([][]float64, len(texts))
	for i := range vectors {
		vectors[i] = []float64{float64(i + 1), 2, 3}
	}
	return vectors, nil
}

// TestPR2459Review_BatchingIsOptInAndGroupsRequests provides a compatibility
// control for the failure-focused tests below. A batch-capable embedder must
// retain the old one-call-per-document path until the load explicitly opts in.
func TestPR2459Review_BatchingIsOptInAndGroupsRequests(t *testing.T) {
	const docCount = 5
	tests := []struct {
		name        string
		batchSize   int
		wantSingles int64
		wantBatches int64
	}{
		{name: "default remains per document", wantSingles: docCount},
		{name: "explicit batching", batchSize: 2, wantBatches: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emb := &pr2459ReviewBatchEmbedder{}
			store := inmemory.New()
			kb := New(
				WithSources([]source.Source{newPR2459ReviewSource("review-source", docCount)}),
				WithVectorStore(store),
				WithEmbedder(emb),
			)
			opts := []LoadOption{
				WithSourceConcurrency(1),
				WithDocConcurrency(2),
				WithShowProgress(false),
				WithShowStats(false),
			}
			if tt.batchSize > 0 {
				opts = append(opts, WithEmbeddingBatchSize(tt.batchSize))
			}

			if err := kb.Load(context.Background(), opts...); err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got := emb.singleCalls.Load(); got != tt.wantSingles {
				t.Errorf("single embedding calls = %d, want %d", got, tt.wantSingles)
			}
			if got := emb.batchCalls.Load(); got != tt.wantBatches {
				t.Errorf("batch embedding calls = %d, want %d", got, tt.wantBatches)
			}
			stored, err := store.Count(context.Background())
			if err != nil {
				t.Fatalf("Count() error = %v", err)
			}
			if stored != docCount {
				t.Errorf("stored documents = %d, want %d", stored, docCount)
			}
		})
	}
}

type pr2459ReviewFailingStore struct {
	vectorstore.VectorStore
	failOnCall int64
	calls      atomic.Int64
	successes  atomic.Int64
}

type pr2459ReviewStatsLogger struct {
	log.Logger

	mu     sync.Mutex
	totals []int
}

func (l *pr2459ReviewStatsLogger) Infof(format string, args ...any) {
	if strings.HasPrefix(format, "Document statistics - total:") && len(args) > 0 {
		if total, ok := args[0].(int); ok {
			l.mu.Lock()
			l.totals = append(l.totals, total)
			l.mu.Unlock()
		}
	}
	l.Logger.Infof(format, args...)
}

func (l *pr2459ReviewStatsLogger) lastTotal() (int, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.totals) == 0 {
		return 0, false
	}
	return l.totals[len(l.totals)-1], true
}

var _ vectorstore.VectorStore = (*pr2459ReviewFailingStore)(nil)

func (s *pr2459ReviewFailingStore) Add(
	ctx context.Context,
	doc *document.Document,
	embedding []float64,
) error {
	call := s.calls.Add(1)
	if call >= s.failOnCall {
		return errPR2459ReviewStoreFailure
	}
	if err := s.VectorStore.Add(ctx, doc, embedding); err != nil {
		return err
	}
	s.successes.Add(1)
	return nil
}

// TestPR2459Review_PartialBatchErrorProgressMatchesPersistedWrites verifies
// that error progress describes the state that was actually persisted. Batch
// storage is intentionally non-transactional, so a failure on the second Add
// leaves the first document in the store and must report one processed
// document rather than the start of the batch.
func TestPR2459Review_PartialBatchErrorProgressMatchesPersistedWrites(t *testing.T) {
	const (
		docCount  = 3
		batchSize = 3
	)

	tests := []struct {
		name           string
		docConcurrency int
		wantStats      int
	}{
		{name: "sequential", docConcurrency: 1, wantStats: docCount},
		{name: "concurrent", docConcurrency: 2, wantStats: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &pr2459ReviewStatsLogger{Logger: log.Default}
			originalLogger := log.Default
			log.Default = logger
			t.Cleanup(func() { log.Default = originalLogger })

			store := &pr2459ReviewFailingStore{
				VectorStore: inmemory.New(),
				failOnCall:  2,
			}
			var (
				mu          sync.Mutex
				errorEvents []LoadProgressEvent
			)
			kb := New(
				WithSources([]source.Source{newPR2459ReviewSource("review-source", docCount)}),
				WithVectorStore(store),
				WithEmbedder(&pr2459ReviewBatchEmbedder{}),
			)

			err := kb.Load(context.Background(),
				WithSourceConcurrency(1),
				WithDocConcurrency(tt.docConcurrency),
				WithEmbeddingBatchSize(batchSize),
				WithShowProgress(false),
				WithShowStats(true),
				WithLoadProgressCallback(func(_ context.Context, event LoadProgressEvent) {
					if event.Err == nil {
						return
					}
					mu.Lock()
					errorEvents = append(errorEvents, event)
					mu.Unlock()
				}),
			)
			if !errors.Is(err, errPR2459ReviewStoreFailure) {
				t.Fatalf("Load() error = %v, want it to wrap %v", err, errPR2459ReviewStoreFailure)
			}

			persisted := int(store.successes.Load())
			if persisted != 1 {
				t.Fatalf("persisted documents = %d, want 1", persisted)
			}

			mu.Lock()
			defer mu.Unlock()
			if len(errorEvents) == 0 {
				t.Fatal("error progress events = 0, want at least 1")
			}
			for i, event := range errorEvents {
				if event.SourceProcessed != persisted {
					t.Errorf("error event %d SourceProcessed = %d, persisted documents = %d",
						i, event.SourceProcessed, persisted)
				}
				if event.Total != persisted {
					t.Errorf("error event %d Total = %d, persisted documents = %d",
						i, event.Total, persisted)
				}
				if event.SourceTotal != docCount {
					t.Errorf("error event %d SourceTotal = %d, want %d", i, event.SourceTotal, docCount)
				}
			}
			statsTotal, ok := logger.lastTotal()
			if !ok {
				t.Fatalf("statistics summary missing, want %d document(s)", tt.wantStats)
			}
			if statsTotal != tt.wantStats {
				t.Errorf("statistics document count = %d, want %d", statsTotal, tt.wantStats)
			}
		})
	}
}

type pr2459ReviewConcurrencyProbe struct {
	entered       chan struct{}
	release       <-chan struct{}
	active        atomic.Int64
	maxConcurrent atomic.Int64
}

func (*pr2459ReviewConcurrencyProbe) GetEmbedding(
	context.Context,
	string,
) ([]float64, error) {
	return []float64{1, 2, 3}, nil
}

func (e *pr2459ReviewConcurrencyProbe) GetEmbeddingWithUsage(
	ctx context.Context,
	text string,
) ([]float64, map[string]any, error) {
	vector, err := e.GetEmbedding(ctx, text)
	return vector, nil, err
}

func (*pr2459ReviewConcurrencyProbe) GetDimensions() int { return 3 }

func (e *pr2459ReviewConcurrencyProbe) GetEmbeddings(
	ctx context.Context,
	texts []string,
) ([][]float64, error) {
	active := e.active.Add(1)
	defer e.active.Add(-1)
	for {
		previous := e.maxConcurrent.Load()
		if active <= previous || e.maxConcurrent.CompareAndSwap(previous, active) {
			break
		}
	}

	select {
	case e.entered <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-e.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	vectors := make([][]float64, len(texts))
	for i := range vectors {
		vectors[i] = []float64{float64(i + 1), 2, 3}
	}
	return vectors, nil
}

// TestPR2459Review_LoadInvokesBatchEmbedderConcurrently is a positive probe
// for the public concurrency contract. It demonstrates deterministically that
// one BatchEmbedder instance receives simultaneous GetEmbeddings calls when
// document concurrency permits multiple batches in flight.
func TestPR2459Review_LoadInvokesBatchEmbedderConcurrently(t *testing.T) {
	const (
		docCount  = 4
		batchSize = 2
	)

	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()

	probe := &pr2459ReviewConcurrencyProbe{
		entered: make(chan struct{}, 2),
		release: release,
	}
	kb := New(
		WithSources([]source.Source{newPR2459ReviewSource("review-source", docCount)}),
		WithVectorStore(inmemory.New()),
		WithEmbedder(probe),
	)
	loadDone := make(chan error, 1)
	go func() {
		loadDone <- kb.Load(context.Background(),
			WithSourceConcurrency(1),
			WithDocConcurrency(2),
			WithEmbeddingBatchSize(batchSize),
			WithShowProgress(false),
			WithShowStats(false),
		)
	}()

	for call := 1; call <= 2; call++ {
		select {
		case <-probe.entered:
		case err := <-loadDone:
			t.Fatalf("Load() completed after %d concurrent call(s): %v", call-1, err)
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for concurrent batch call %d", call)
		}
	}
	unblock()

	select {
	case err := <-loadDone:
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Load() to finish")
	}
	if got := probe.maxConcurrent.Load(); got < 2 {
		t.Errorf("maximum concurrent GetEmbeddings calls = %d, want at least 2", got)
	}
}
