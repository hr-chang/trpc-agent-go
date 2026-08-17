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
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

type pr2459MatrixRecordingEmbedder struct {
	mu      sync.Mutex
	singles []string
	batches [][]string
}

func pr2459MatrixVectorForText(text string) []float64 {
	sum := 0
	for _, r := range text {
		sum += int(r)
	}
	return []float64{float64(len([]rune(text))), float64(sum)}
}

func (e *pr2459MatrixRecordingEmbedder) GetEmbedding(
	ctx context.Context,
	text string,
) ([]float64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.mu.Lock()
	e.singles = append(e.singles, text)
	e.mu.Unlock()
	return pr2459MatrixVectorForText(text), nil
}

func (e *pr2459MatrixRecordingEmbedder) GetEmbeddingWithUsage(
	ctx context.Context,
	text string,
) ([]float64, map[string]any, error) {
	vector, err := e.GetEmbedding(ctx, text)
	return vector, nil, err
}

func (*pr2459MatrixRecordingEmbedder) GetDimensions() int { return 2 }

func (e *pr2459MatrixRecordingEmbedder) GetEmbeddings(
	ctx context.Context,
	texts []string,
) ([][]float64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.mu.Lock()
	e.batches = append(e.batches, slices.Clone(texts))
	e.mu.Unlock()
	vectors := make([][]float64, len(texts))
	for i, text := range texts {
		vectors[i] = pr2459MatrixVectorForText(text)
	}
	return vectors, nil
}

func (e *pr2459MatrixRecordingEmbedder) snapshot() ([]string, [][]string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	singles := slices.Clone(e.singles)
	batches := make([][]string, len(e.batches))
	for i := range e.batches {
		batches[i] = slices.Clone(e.batches[i])
	}
	return singles, batches
}

// TestPR2459Matrix_BatchedAndPerDocumentPathsAreEquivalent compares the old
// path and the opt-in batch path as a black-box A/B. The exact embedding text,
// stored document set, and document-to-vector binding must remain identical.
func TestPR2459Matrix_BatchedAndPerDocumentPathsAreEquivalent(t *testing.T) {
	docs := []*document.Document{
		{ID: "plain", Name: "Plain", Content: "plain content"},
		{
			ID:      "metadata",
			Name:    "Metadata",
			Content: "metadata content",
			Metadata: map[string]any{
				source.MetaFileName:           "matrix.md",
				source.MetaChunkIndex:         2,
				source.MetaMarkdownHeaderPath: "Root / Child",
			},
		},
		{
			ID:            "override",
			Name:          "Override",
			Content:       "content that must not be embedded",
			EmbeddingText: "custom embedding text",
		},
		{ID: "unicode", Name: "Unicode", Content: "重复内容 — café"},
	}
	src := &pr2459ReviewSource{name: "matrix-source", docs: docs}

	type result struct {
		inputs  []string
		vectors map[string][]float64
	}
	run := func(t *testing.T, batchSize int) result {
		t.Helper()
		emb := &pr2459MatrixRecordingEmbedder{}
		store := inmemory.New()
		kb := New(
			WithSources([]source.Source{src}),
			WithVectorStore(store),
			WithEmbedder(emb),
		)
		opts := []LoadOption{
			WithSourceConcurrency(1),
			WithDocConcurrency(1),
			WithShowProgress(false),
			WithShowStats(false),
		}
		if batchSize > 0 {
			opts = append(opts, WithEmbeddingBatchSize(batchSize))
		}
		if err := kb.Load(context.Background(), opts...); err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		singles, batches := emb.snapshot()
		inputs := slices.Clone(singles)
		for _, batch := range batches {
			inputs = append(inputs, batch...)
		}
		vectors := make(map[string][]float64, len(docs))
		for _, doc := range docs {
			_, vector, err := store.Get(context.Background(), doc.ID)
			if err != nil {
				t.Fatalf("Get(%q) error = %v", doc.ID, err)
			}
			vectors[doc.ID] = vector
		}
		return result{inputs: inputs, vectors: vectors}
	}

	perDocument := run(t, 0)
	batched := run(t, 2)
	if !slices.Equal(perDocument.inputs, batched.inputs) {
		t.Errorf("embedding inputs differ:\nper-document = %q\nbatched      = %q",
			perDocument.inputs, batched.inputs)
	}
	for _, doc := range docs {
		if !slices.Equal(perDocument.vectors[doc.ID], batched.vectors[doc.ID]) {
			t.Errorf("document %q vector differs: per-document=%v batched=%v",
				doc.ID, perDocument.vectors[doc.ID], batched.vectors[doc.ID])
		}
	}
}

// TestPR2459Matrix_BatchShapeAcrossSizesAndConcurrency covers empty, exact,
// trailing, oversized, disabled, and negative batch sizes on both loader paths.
func TestPR2459Matrix_BatchShapeAcrossSizesAndConcurrency(t *testing.T) {
	docCounts := []int{0, 1, 2, 5, 8}
	batchSizes := []int{-2, 0, 1, 2, 3, 10}
	concurrencies := []int{1, 4}

	for _, docCount := range docCounts {
		for _, batchSize := range batchSizes {
			for _, concurrency := range concurrencies {
				name := fmt.Sprintf("docs=%d/batch=%d/concurrency=%d",
					docCount, batchSize, concurrency)
				t.Run(name, func(t *testing.T) {
					emb := &pr2459MatrixRecordingEmbedder{}
					store := inmemory.New()
					kb := New(
						WithSources([]source.Source{newPR2459ReviewSource("matrix-source", docCount)}),
						WithVectorStore(store),
						WithEmbedder(emb),
					)
					if err := kb.Load(context.Background(),
						WithSourceConcurrency(1),
						WithDocConcurrency(concurrency),
						WithEmbeddingBatchSize(batchSize),
						WithShowProgress(false),
						WithShowStats(false),
					); err != nil {
						t.Fatalf("Load() error = %v", err)
					}

					singles, batches := emb.snapshot()
					if batchSize <= 1 {
						if len(singles) != docCount || len(batches) != 0 {
							t.Errorf("single calls=%d batch calls=%d, want %d and 0",
								len(singles), len(batches), docCount)
						}
					} else {
						if len(singles) != 0 {
							t.Errorf("single calls=%d, want 0", len(singles))
						}
						gotSizes := make([]int, len(batches))
						for i, batch := range batches {
							gotSizes[i] = len(batch)
						}
						sort.Ints(gotSizes)
						var wantSizes []int
						for start := 0; start < docCount; start += batchSize {
							wantSizes = append(wantSizes, min(batchSize, docCount-start))
						}
						sort.Ints(wantSizes)
						if !slices.Equal(gotSizes, wantSizes) {
							t.Errorf("batch sizes=%v, want %v", gotSizes, wantSizes)
						}
					}
					stored, err := store.Count(context.Background())
					if err != nil {
						t.Fatalf("Count() error = %v", err)
					}
					if stored != docCount {
						t.Errorf("stored documents=%d, want %d", stored, docCount)
					}
				})
			}
		}
	}
}

// TestPR2459Matrix_BatchOptionIsScopedToOneLoad verifies that an opt-in batch
// plan is rebuilt for every Load call and cannot leak into a later default run.
func TestPR2459Matrix_BatchOptionIsScopedToOneLoad(t *testing.T) {
	const docCount = 5
	emb := &pr2459MatrixRecordingEmbedder{}
	store := inmemory.New()
	kb := New(
		WithSources([]source.Source{newPR2459ReviewSource("matrix-source", docCount)}),
		WithVectorStore(store),
		WithEmbedder(emb),
	)
	base := []LoadOption{
		WithSourceConcurrency(1),
		WithDocConcurrency(1),
		WithShowProgress(false),
		WithShowStats(false),
	}

	if err := kb.Load(context.Background(), append(base, WithEmbeddingBatchSize(2))...); err != nil {
		t.Fatalf("first batched Load() error = %v", err)
	}
	singles, batches := emb.snapshot()
	if len(singles) != 0 || len(batches) != 3 {
		t.Fatalf("after first load: singles=%d batches=%d, want 0 and 3", len(singles), len(batches))
	}

	if err := kb.Load(context.Background(), base...); err != nil {
		t.Fatalf("default Load() error = %v", err)
	}
	singles, batches = emb.snapshot()
	if len(singles) != docCount || len(batches) != 3 {
		t.Fatalf("after default load: singles=%d batches=%d, want %d and 3",
			len(singles), len(batches), docCount)
	}

	if err := kb.Load(context.Background(), append(base, WithEmbeddingBatchSize(10))...); err != nil {
		t.Fatalf("second batched Load() error = %v", err)
	}
	singles, batches = emb.snapshot()
	if len(singles) != docCount || len(batches) != 4 {
		t.Errorf("after second batched load: singles=%d batches=%d, want %d and 4",
			len(singles), len(batches), docCount)
	}
}

type pr2459MatrixRemoteStore struct {
	vectorstore.VectorStore
	mu        sync.Mutex
	emptyAdds int
}

func (s *pr2459MatrixRemoteStore) Add(
	ctx context.Context,
	doc *document.Document,
	embedding []float64,
) error {
	if len(embedding) != 0 {
		return fmt.Errorf("remote store received local embedding %v", embedding)
	}
	s.mu.Lock()
	s.emptyAdds++
	s.mu.Unlock()
	// The in-memory delegate needs a non-empty vector; it is only used to
	// retain and count documents after the wrapper validates the remote shape.
	return s.VectorStore.Add(ctx, doc, []float64{1})
}

func (s *pr2459MatrixRemoteStore) emptyAddCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.emptyAdds
}

// TestPR2459Matrix_NoEmbedderPreservesRemoteEmbeddingFallback verifies the
// documented no-embedder path with batching configured on both loader modes.
func TestPR2459Matrix_NoEmbedderPreservesRemoteEmbeddingFallback(t *testing.T) {
	const docCount = 6
	for _, concurrency := range []int{1, 3} {
		t.Run(fmt.Sprintf("concurrency=%d", concurrency), func(t *testing.T) {
			var batchMessages []string
			originalInfof := log.InfofContext
			log.InfofContext = func(_ context.Context, format string, args ...any) {
				message := fmt.Sprintf(format, args...)
				if strings.Contains(message, "Embedding batch size") {
					batchMessages = append(batchMessages, message)
				}
			}
			t.Cleanup(func() { log.InfofContext = originalInfof })

			store := &pr2459MatrixRemoteStore{VectorStore: inmemory.New()}
			kb := New(
				WithSources([]source.Source{newPR2459ReviewSource("remote-source", docCount)}),
				WithVectorStore(store),
			)
			if err := kb.Load(context.Background(),
				WithSourceConcurrency(1),
				WithDocConcurrency(concurrency),
				WithEmbeddingBatchSize(4),
				WithShowProgress(false),
				WithShowStats(false),
			); err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got := store.emptyAddCount(); got != docCount {
				t.Errorf("remote empty-vector adds=%d, want %d", got, docCount)
			}
			if len(batchMessages) != 1 {
				t.Fatalf("batch configuration messages=%q, want exactly one", batchMessages)
			}
			if !strings.Contains(batchMessages[0], "no embedder is configured") {
				t.Errorf("batch configuration message=%q, want missing-embedder reason",
					batchMessages[0])
			}
			if strings.Contains(batchMessages[0], "does not implement BatchEmbedder") {
				t.Errorf("batch configuration message=%q incorrectly reports an unsupported embedder",
					batchMessages[0])
			}
		})
	}
}

// TestPR2459Matrix_ProgressBoundariesAndTotals covers step sizes smaller than,
// equal to, and larger than batches, plus oversized batches and empty sources.
func TestPR2459Matrix_ProgressBoundariesAndTotals(t *testing.T) {
	tests := []struct {
		name      string
		docCount  int
		batchSize int
		step      int
		want      []int
	}{
		{name: "every batch", docCount: 5, batchSize: 2, step: 0, want: []int{2, 4, 5}},
		{name: "batch crosses boundaries", docCount: 10, batchSize: 4, step: 3, want: []int{4, 8, 10}},
		{name: "several batches before boundary", docCount: 7, batchSize: 2, step: 5, want: []int{6, 7}},
		{name: "oversized batch", docCount: 3, batchSize: 10, step: 2, want: []int{3}},
		{name: "empty source", docCount: 0, batchSize: 4, step: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var events []LoadProgressEvent
			kb := New(
				WithSources([]source.Source{newPR2459ReviewSource("progress-source", tt.docCount)}),
				WithVectorStore(inmemory.New()),
				WithEmbedder(&pr2459MatrixRecordingEmbedder{}),
			)
			if err := kb.Load(context.Background(),
				WithSourceConcurrency(1),
				WithDocConcurrency(1),
				WithEmbeddingBatchSize(tt.batchSize),
				WithProgressStepSize(tt.step),
				WithShowProgress(false),
				WithShowStats(false),
				WithLoadProgressCallback(func(_ context.Context, event LoadProgressEvent) {
					events = append(events, event)
				}),
			); err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			var progress []int
			doneCount := 0
			for _, event := range events {
				if event.Done {
					doneCount++
					if event.Total != tt.docCount {
						t.Errorf("done Total=%d, want %d", event.Total, tt.docCount)
					}
					continue
				}
				if event.Err != nil {
					t.Errorf("unexpected progress error: %v", event.Err)
					continue
				}
				progress = append(progress, event.SourceProcessed)
				if event.SourceTotal != tt.docCount {
					t.Errorf("SourceTotal=%d, want %d", event.SourceTotal, tt.docCount)
				}
				if event.Total != event.SourceProcessed {
					t.Errorf("single-source Total=%d, SourceProcessed=%d",
						event.Total, event.SourceProcessed)
				}
			}
			if !slices.Equal(progress, tt.want) {
				t.Errorf("progress=%v, want %v", progress, tt.want)
			}
			if doneCount != 1 {
				t.Errorf("done events=%d, want 1", doneCount)
			}
		})
	}
}

func newPR2459MatrixNamedSource(name string, count int) *pr2459ReviewSource {
	src := newPR2459ReviewSource(name, count)
	for i, doc := range src.docs {
		doc.ID = fmt.Sprintf("%s-doc-%d", name, i)
		doc.Content = fmt.Sprintf("%s content %d", name, i)
		doc.EmbeddingText = fmt.Sprintf("%s embedding %d", name, i)
		doc.Metadata[source.MetaURI] = fmt.Sprintf("matrix://%s/%d", name, i)
	}
	return src
}

// TestPR2459Matrix_MultiSourceTotalsAndIsolation verifies aggregate progress
// and request grouping with an empty source, uneven source sizes, and both
// sequential and concurrent loading. No embedding request may mix sources.
func TestPR2459Matrix_MultiSourceTotalsAndIsolation(t *testing.T) {
	counts := []int{0, 2, 5, 1}
	names := []string{"empty", "alpha", "beta", "gamma"}
	sources := make([]source.Source, len(counts))
	totalDocs := 0
	for i := range counts {
		sources[i] = newPR2459MatrixNamedSource(names[i], counts[i])
		totalDocs += counts[i]
	}

	tests := []struct {
		name              string
		sourceConcurrency int
		docConcurrency    int
		wantExactTotals   []int
	}{
		{
			name:              "sequential",
			sourceConcurrency: 1,
			docConcurrency:    1,
			wantExactTotals:   []int{2, 5, 7, 8},
		},
		{name: "concurrent", sourceConcurrency: 3, docConcurrency: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emb := &pr2459MatrixRecordingEmbedder{}
			store := inmemory.New()
			var (
				mu     sync.Mutex
				events []LoadProgressEvent
			)
			kb := New(
				WithSources(sources),
				WithVectorStore(store),
				WithEmbedder(emb),
			)
			if err := kb.Load(context.Background(),
				WithSourceConcurrency(tt.sourceConcurrency),
				WithDocConcurrency(tt.docConcurrency),
				WithEmbeddingBatchSize(3),
				WithProgressStepSize(0),
				WithShowProgress(false),
				WithShowStats(false),
				WithLoadProgressCallback(func(_ context.Context, event LoadProgressEvent) {
					mu.Lock()
					events = append(events, event)
					mu.Unlock()
				}),
			); err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			stored, err := store.Count(context.Background())
			if err != nil {
				t.Fatalf("Count() error = %v", err)
			}
			if stored != totalDocs {
				t.Errorf("stored documents=%d, want %d", stored, totalDocs)
			}
			_, batches := emb.snapshot()
			gotSizes := make([]int, len(batches))
			for i, batch := range batches {
				gotSizes[i] = len(batch)
				prefix := ""
				for _, text := range batch {
					parts := strings.SplitN(text, " ", 2)
					if len(parts) == 0 {
						t.Fatalf("empty embedding text in batch %d", i)
					}
					if prefix == "" {
						prefix = parts[0]
					} else if parts[0] != prefix {
						t.Errorf("batch %d mixes source prefixes %q and %q", i, prefix, parts[0])
					}
				}
			}
			sort.Ints(gotSizes)
			wantSizes := []int{1, 2, 2, 3}
			if !slices.Equal(gotSizes, wantSizes) {
				t.Errorf("multi-source batch sizes=%v, want %v", gotSizes, wantSizes)
			}

			mu.Lock()
			defer mu.Unlock()
			var (
				progressTotals []int
				doneCount      int
			)
			for _, event := range events {
				if !slices.Equal(event.SourceNames, names) {
					t.Errorf("SourceNames=%v, want %v", event.SourceNames, names)
				}
				if event.Done {
					doneCount++
					if event.Total != totalDocs {
						t.Errorf("done Total=%d, want %d", event.Total, totalDocs)
					}
					continue
				}
				if event.Err != nil {
					t.Errorf("unexpected progress error: %v", event.Err)
					continue
				}
				progressTotals = append(progressTotals, event.Total)
				if event.Total < event.SourceProcessed || event.Total > totalDocs {
					t.Errorf("event Total=%d outside [%d,%d]", event.Total, event.SourceProcessed, totalDocs)
				}
			}
			if doneCount != 1 {
				t.Errorf("done events=%d, want 1", doneCount)
			}
			if tt.wantExactTotals != nil && !slices.Equal(progressTotals, tt.wantExactTotals) {
				t.Errorf("sequential progress totals=%v, want %v",
					progressTotals, tt.wantExactTotals)
			}
		})
	}
}

// TestPR2459Matrix_SequentialPartialWritesAcrossBatchPositions ensures error
// progress remains faithful when the failure lands in the first, middle, or
// trailing batch rather than only at one hand-picked position.
func TestPR2459Matrix_SequentialPartialWritesAcrossBatchPositions(t *testing.T) {
	tests := []struct {
		name       string
		docCount   int
		batchSize  int
		failOnCall int64
		wantStored int
	}{
		{name: "first batch", docCount: 5, batchSize: 5, failOnCall: 2, wantStored: 1},
		{name: "middle batch", docCount: 7, batchSize: 3, failOnCall: 5, wantStored: 4},
		{name: "trailing batch", docCount: 8, batchSize: 3, failOnCall: 8, wantStored: 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &pr2459ReviewFailingStore{
				VectorStore: inmemory.New(),
				failOnCall:  tt.failOnCall,
			}
			var errorEvents []LoadProgressEvent
			kb := New(
				WithSources([]source.Source{newPR2459ReviewSource("partial-source", tt.docCount)}),
				WithVectorStore(store),
				WithEmbedder(&pr2459MatrixRecordingEmbedder{}),
			)
			err := kb.Load(context.Background(),
				WithSourceConcurrency(1),
				WithDocConcurrency(1),
				WithEmbeddingBatchSize(tt.batchSize),
				WithShowProgress(false),
				WithShowStats(false),
				WithLoadProgressCallback(func(_ context.Context, event LoadProgressEvent) {
					if event.Err != nil {
						errorEvents = append(errorEvents, event)
					}
				}),
			)
			if !errors.Is(err, errPR2459ReviewStoreFailure) {
				t.Fatalf("Load() error=%v, want it to wrap %v", err, errPR2459ReviewStoreFailure)
			}
			if got := int(store.successes.Load()); got != tt.wantStored {
				t.Fatalf("persisted documents=%d, want %d", got, tt.wantStored)
			}
			if len(errorEvents) != 1 {
				t.Fatalf("error events=%d, want 1", len(errorEvents))
			}
			event := errorEvents[0]
			if event.SourceProcessed != tt.wantStored {
				t.Errorf("SourceProcessed=%d, persisted=%d", event.SourceProcessed, tt.wantStored)
			}
			if event.Total != tt.wantStored {
				t.Errorf("Total=%d, persisted=%d", event.Total, tt.wantStored)
			}
		})
	}
}
