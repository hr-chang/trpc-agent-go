//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

package pr2459legacycompat

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	openaioption "github.com/openai/openai-go/option"
	"trpc.group/trpc-go/trpc-agent-go/knowledge"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	openaiembedder "trpc.group/trpc-go/trpc-agent-go/knowledge/embedder/openai"
	knowledgesource "trpc.group/trpc-go/trpc-agent-go/knowledge/source"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore"
)

// legacyCompatibilityGoldenSHA256 is frozen from the base revision. The same
// source is compiled against both base.mod and head.mod, so a passing head run
// proves that the canonical legacy trace still matches the base behavior.
const legacyCompatibilityGoldenSHA256 = "e5ad896f48dae2b2daf112b041c530441bbd68610e84851b0a5b85e407d1e01b"

type compatibilityTrace struct {
	Knowledge []knowledgeScenarioTrace `json:"knowledge"`
	OpenAI    []openAITrace            `json:"openai"`
}

type knowledgeScenarioTrace struct {
	Name            string                `json:"name"`
	Result          string                `json:"result"`
	CauseMatched    bool                  `json:"cause_matched,omitempty"`
	SourceReads     []string              `json:"source_reads"`
	EmbeddingCalls  []string              `json:"embedding_calls"`
	BatchCalls      [][]string            `json:"batch_embedding_calls"`
	StoreOperations []storeOperationTrace `json:"store_operations"`
	Progress        []progressEventTrace  `json:"progress"`
	FinalDocuments  []storedDocumentTrace `json:"final_documents"`
	Canonicalized   bool                  `json:"canonicalized,omitempty"`
}

type progressEventTrace struct {
	SourceNames     []string `json:"source_names,omitempty"`
	SourceName      string   `json:"source_name,omitempty"`
	SourceProcessed int      `json:"source_processed,omitempty"`
	SourceTotal     int      `json:"source_total,omitempty"`
	Total           int      `json:"total,omitempty"`
	Done            bool     `json:"done,omitempty"`
	Error           string   `json:"error,omitempty"`
}

type documentTrace struct {
	ID            string    `json:"id"`
	Name          string    `json:"name,omitempty"`
	Content       string    `json:"content"`
	EmbeddingText string    `json:"embedding_text,omitempty"`
	Metadata      string    `json:"metadata,omitempty"`
	Embedding     []float64 `json:"embedding"`
}

type storeOperationTrace struct {
	Kind     string         `json:"kind"`
	Document *documentTrace `json:"document,omitempty"`
	ID       string         `json:"id,omitempty"`
	IDs      []string       `json:"ids,omitempty"`
	Filter   string         `json:"filter,omitempty"`
	Count    int            `json:"count,omitempty"`
	Error    string         `json:"error,omitempty"`
}

type storedDocumentTrace struct {
	Document  documentTrace `json:"document"`
	Embedding []float64     `json:"embedding"`
}

type openAITrace struct {
	Name         string             `json:"name"`
	Requests     []httpRequestTrace `json:"requests"`
	Vector       []float64          `json:"vector,omitempty"`
	Usage        string             `json:"usage,omitempty"`
	Error        string             `json:"error,omitempty"`
	CauseMatched bool               `json:"cause_matched,omitempty"`
	Attempts     int64              `json:"attempts,omitempty"`
}

type httpRequestTrace struct {
	Path          string `json:"path"`
	Authorization string `json:"authorization,omitempty"`
	ContentType   string `json:"content_type,omitempty"`
	CustomHeader  string `json:"custom_header,omitempty"`
	Body          string `json:"body"`
}

type scenarioRecorder struct {
	mu              sync.Mutex
	sourceReads     []string
	embeddingCalls  []string
	batchCalls      [][]string
	storeOperations []storeOperationTrace
	progress        []progressEventTrace
}

func (r *scenarioRecorder) recordSourceRead(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sourceReads = append(r.sourceReads, name)
}

func (r *scenarioRecorder) recordEmbeddingCall(text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.embeddingCalls = append(r.embeddingCalls, text)
}

func (r *scenarioRecorder) recordBatchCall(texts []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.batchCalls = append(r.batchCalls, append([]string(nil), texts...))
}

func (r *scenarioRecorder) recordStoreOperation(operation storeOperationTrace) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.storeOperations = append(r.storeOperations, operation)
}

func (r *scenarioRecorder) recordProgress(event knowledge.LoadProgressEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.progress = append(r.progress, progressEventTrace{
		SourceNames:     append([]string(nil), event.SourceNames...),
		SourceName:      event.SourceName,
		SourceProcessed: event.SourceProcessed,
		SourceTotal:     event.SourceTotal,
		Total:           event.Total,
		Done:            event.Done,
		Error:           errorText(event.Err),
	})
}

func (r *scenarioRecorder) snapshot(canonicalizeConcurrent bool) (
	[]string,
	[]string,
	[][]string,
	[]storeOperationTrace,
	[]progressEventTrace,
) {
	r.mu.Lock()
	defer r.mu.Unlock()

	sourceReads := append([]string(nil), r.sourceReads...)
	embeddingCalls := append([]string(nil), r.embeddingCalls...)
	batchCalls := make([][]string, 0, len(r.batchCalls))
	for _, call := range r.batchCalls {
		batchCalls = append(batchCalls, append([]string(nil), call...))
	}
	storeOperations := append([]storeOperationTrace(nil), r.storeOperations...)
	progress := append([]progressEventTrace(nil), r.progress...)
	if !canonicalizeConcurrent {
		return sourceReads, embeddingCalls, batchCalls, storeOperations, progress
	}

	sort.Strings(sourceReads)
	sort.Strings(embeddingCalls)
	sort.Slice(batchCalls, func(i, j int) bool {
		return canonicalJSON(batchCalls[i]) < canonicalJSON(batchCalls[j])
	})
	sort.Slice(storeOperations, func(i, j int) bool {
		return canonicalJSON(storeOperations[i]) < canonicalJSON(storeOperations[j])
	})
	for i := range progress {
		// Global totals depend on goroutine scheduling. Per-source counts and the
		// final document set are the stable concurrent contract under test.
		progress[i].Total = 0
	}
	sort.Slice(progress, func(i, j int) bool {
		return canonicalJSON(progress[i]) < canonicalJSON(progress[j])
	})
	return sourceReads, embeddingCalls, batchCalls, storeOperations, progress
}

type legacySource struct {
	recorder *scenarioRecorder
	name     string
	docs     []*document.Document
	metadata map[string]any
	err      error
}

func (s *legacySource) ReadDocuments(ctx context.Context) ([]*document.Document, error) {
	s.recorder.recordSourceRead(s.name)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.err != nil {
		return nil, s.err
	}
	docs := make([]*document.Document, 0, len(s.docs))
	for _, doc := range s.docs {
		docs = append(docs, doc.Clone())
	}
	return docs, nil
}

func (s *legacySource) Name() string { return s.name }

func (s *legacySource) Type() string { return "legacy-compat" }

func (s *legacySource) GetMetadata() map[string]any {
	metadata := make(map[string]any, len(s.metadata))
	for key, value := range s.metadata {
		metadata[key] = value
	}
	return metadata
}

type legacyEmbedder struct {
	recorder *scenarioRecorder
	failText string
	failErr  error
}

func (e *legacyEmbedder) GetEmbedding(ctx context.Context, text string) ([]float64, error) {
	e.recorder.recordEmbeddingCall(text)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if text == e.failText {
		return nil, e.failErr
	}
	return vectorForText(text), nil
}

func (e *legacyEmbedder) GetEmbeddingWithUsage(
	ctx context.Context,
	text string,
) ([]float64, map[string]any, error) {
	vector, err := e.GetEmbedding(ctx, text)
	if err != nil {
		return nil, nil, err
	}
	return vector, map[string]any{"prompt_tokens": len([]rune(text))}, nil
}

// GetEmbeddings intentionally gives this pre-PR-style test double a structural
// batch capability. The base revision ignores the extra method. The PR head
// recognizes it, but without the new opt-in option it must still call only
// GetEmbedding, leaving BatchCalls empty in both traces.
func (e *legacyEmbedder) GetEmbeddings(ctx context.Context, texts []string) ([][]float64, error) {
	e.recorder.recordBatchCall(texts)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	vectors := make([][]float64, 0, len(texts))
	for _, text := range texts {
		if text == e.failText {
			return nil, e.failErr
		}
		vectors = append(vectors, vectorForText(text))
	}
	return vectors, nil
}

func (e *legacyEmbedder) GetDimensions() int { return 2 }

type storedItem struct {
	document  *document.Document
	embedding []float64
}

type legacyVectorStore struct {
	mu       sync.Mutex
	recorder *scenarioRecorder
	items    map[string]storedItem
	failID   string
	failErr  error
}

func newLegacyVectorStore(recorder *scenarioRecorder) *legacyVectorStore {
	return &legacyVectorStore{
		recorder: recorder,
		items:    make(map[string]storedItem),
	}
}

func (s *legacyVectorStore) preload(doc *document.Document, embedding []float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[doc.ID] = storedItem{document: doc.Clone(), embedding: append([]float64(nil), embedding...)}
}

func (s *legacyVectorStore) Add(
	_ context.Context,
	doc *document.Document,
	embedding []float64,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	operation := storeOperationTrace{
		Kind: "add",
		Document: &documentTrace{
			ID:            doc.ID,
			Name:          doc.Name,
			Content:       doc.Content,
			EmbeddingText: doc.EmbeddingText,
			Metadata:      canonicalJSON(doc.Metadata),
			Embedding:     append([]float64(nil), embedding...),
		},
	}
	if doc.ID == s.failID {
		operation.Error = errorText(s.failErr)
		s.recorder.recordStoreOperation(operation)
		return s.failErr
	}
	s.items[doc.ID] = storedItem{
		document:  doc.Clone(),
		embedding: append([]float64(nil), embedding...),
	}
	s.recorder.recordStoreOperation(operation)
	return nil
}

func (s *legacyVectorStore) Get(
	_ context.Context,
	id string,
) (*document.Document, []float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return nil, nil, fmt.Errorf("document %s not found", id)
	}
	return item.document.Clone(), append([]float64(nil), item.embedding...), nil
}

func (s *legacyVectorStore) Update(
	_ context.Context,
	doc *document.Document,
	embedding []float64,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[doc.ID] = storedItem{
		document:  doc.Clone(),
		embedding: append([]float64(nil), embedding...),
	}
	s.recorder.recordStoreOperation(storeOperationTrace{Kind: "update", ID: doc.ID})
	return nil
}

func (s *legacyVectorStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, id)
	s.recorder.recordStoreOperation(storeOperationTrace{Kind: "delete", ID: id})
	return nil
}

func (s *legacyVectorStore) Search(
	context.Context,
	*vectorstore.SearchQuery,
) (*vectorstore.SearchResult, error) {
	return &vectorstore.SearchResult{}, nil
}

func (s *legacyVectorStore) DeleteByFilter(
	_ context.Context,
	opts ...vectorstore.DeleteOption,
) error {
	config := vectorstore.ApplyDeleteOptions(opts...)
	s.mu.Lock()
	defer s.mu.Unlock()

	ids := append([]string(nil), config.DocumentIDs...)
	if config.DeleteAll {
		ids = ids[:0]
		for id := range s.items {
			ids = append(ids, id)
		}
	}
	if len(config.Filter) > 0 {
		ids = ids[:0]
		for id, item := range s.items {
			if metadataMatches(item.document.Metadata, config.Filter) {
				ids = append(ids, id)
			}
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		delete(s.items, id)
	}
	s.recorder.recordStoreOperation(storeOperationTrace{
		Kind:   "delete-by-filter",
		IDs:    ids,
		Filter: canonicalJSON(config.Filter),
	})
	return nil
}

func (s *legacyVectorStore) UpdateByFilter(
	context.Context,
	...vectorstore.UpdateByFilterOption,
) (int64, error) {
	return 0, nil
}

func (s *legacyVectorStore) Count(
	_ context.Context,
	opts ...vectorstore.CountOption,
) (int, error) {
	config := vectorstore.ApplyCountOptions(opts...)
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, item := range s.items {
		if metadataMatches(item.document.Metadata, config.Filter) {
			count++
		}
	}
	s.recorder.recordStoreOperation(storeOperationTrace{
		Kind:   "count",
		Filter: canonicalJSON(config.Filter),
		Count:  count,
	})
	return count, nil
}

func (s *legacyVectorStore) GetMetadata(
	_ context.Context,
	opts ...vectorstore.GetMetadataOption,
) (map[string]vectorstore.DocumentMetadata, error) {
	config, err := vectorstore.ApplyGetMetadataOptions(opts...)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	selected := make([]string, 0)
	requestedIDs := make(map[string]struct{}, len(config.IDs))
	for _, id := range config.IDs {
		requestedIDs[id] = struct{}{}
	}
	for id, item := range s.items {
		if len(requestedIDs) > 0 {
			if _, ok := requestedIDs[id]; !ok {
				continue
			}
		}
		if metadataMatches(item.document.Metadata, config.Filter) {
			selected = append(selected, id)
		}
	}
	sort.Strings(selected)
	start := config.Offset
	if start < 0 {
		start = 0
	}
	if start > len(selected) {
		start = len(selected)
	}
	end := len(selected)
	if config.Limit > 0 && start+config.Limit < end {
		end = start + config.Limit
	}
	selected = selected[start:end]

	result := make(map[string]vectorstore.DocumentMetadata, len(selected))
	for _, id := range selected {
		metadata := make(map[string]any, len(s.items[id].document.Metadata))
		for key, value := range s.items[id].document.Metadata {
			metadata[key] = value
		}
		result[id] = vectorstore.DocumentMetadata{Metadata: metadata}
	}
	ids := append([]string(nil), config.IDs...)
	sort.Strings(ids)
	s.recorder.recordStoreOperation(storeOperationTrace{
		Kind:   "get-metadata",
		IDs:    ids,
		Filter: canonicalJSON(config.Filter),
		Count:  len(result),
	})
	return result, nil
}

func (s *legacyVectorStore) Close() error {
	s.recorder.recordStoreOperation(storeOperationTrace{Kind: "close"})
	return nil
}

func (s *legacyVectorStore) finalDocuments() []storedDocumentTrace {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.items))
	for id := range s.items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]storedDocumentTrace, 0, len(ids))
	for _, id := range ids {
		item := s.items[id]
		result = append(result, storedDocumentTrace{
			Document: documentTrace{
				ID:            item.document.ID,
				Name:          item.document.Name,
				Content:       item.document.Content,
				EmbeddingText: item.document.EmbeddingText,
				Metadata:      canonicalJSON(item.document.Metadata),
			},
			Embedding: append([]float64(nil), item.embedding...),
		})
	}
	return result
}

type knowledgeLoadCase struct {
	name          string
	sources       []*legacySource
	embedder      *legacyEmbedder
	store         *legacyVectorStore
	enableSync    bool
	sourceWorkers int
	docWorkers    int
	progressStep  int
	cause         error
	canonicalize  bool
}

func runKnowledgeLoad(testCase knowledgeLoadCase) knowledgeScenarioTrace {
	options := []knowledge.Option{
		knowledge.WithVectorStore(testCase.store),
	}
	sources := make([]knowledgesource.Source, 0, len(testCase.sources))
	for _, source := range testCase.sources {
		sources = append(sources, source)
	}
	options = append(options, knowledge.WithSources(sources))
	if testCase.embedder != nil {
		options = append(options, knowledge.WithEmbedder(testCase.embedder))
	}
	if testCase.enableSync {
		options = append(options, knowledge.WithEnableSourceSync(true))
	}

	kb := knowledge.New(options...)
	err := kb.Load(
		context.Background(),
		knowledge.WithSourceConcurrency(testCase.sourceWorkers),
		knowledge.WithDocConcurrency(testCase.docWorkers),
		knowledge.WithShowProgress(false),
		knowledge.WithShowStats(false),
		knowledge.WithProgressStepSize(testCase.progressStep),
		knowledge.WithLoadProgressCallback(func(_ context.Context, event knowledge.LoadProgressEvent) {
			testCase.store.recorder.recordProgress(event)
		}),
	)

	sourceReads, embeddingCalls, batchCalls, operations, progress :=
		testCase.store.recorder.snapshot(testCase.canonicalize)
	return knowledgeScenarioTrace{
		Name:            testCase.name,
		Result:          errorText(err),
		CauseMatched:    testCase.cause != nil && errors.Is(err, testCase.cause),
		SourceReads:     sourceReads,
		EmbeddingCalls:  embeddingCalls,
		BatchCalls:      batchCalls,
		StoreOperations: operations,
		Progress:        progress,
		FinalDocuments:  testCase.store.finalDocuments(),
		Canonicalized:   testCase.canonicalize,
	}
}

func buildKnowledgeTrace() []knowledgeScenarioTrace {
	var traces []knowledgeScenarioTrace

	sequentialRecorder := &scenarioRecorder{}
	sequentialStore := newLegacyVectorStore(sequentialRecorder)
	traces = append(traces, runKnowledgeLoad(knowledgeLoadCase{
		name: "sequential-success-positive-progress-step",
		sources: []*legacySource{
			{
				recorder: sequentialRecorder,
				name:     "alpha",
				docs: []*document.Document{
					compatDocument("a-0", "Alpha zero", map[string]any{
						knowledgesource.MetaFileName:           "alpha.md",
						knowledgesource.MetaChunkIndex:         0,
						knowledgesource.MetaMarkdownHeaderPath: "Intro",
						"tenant":                               "blue",
					}),
					compatDocumentWithEmbeddingText("a-1", "Alpha one", "CUSTOM::alpha-one"),
					compatDocument("a-2", "重复内容", nil),
				},
			},
			{
				recorder: sequentialRecorder,
				name:     "beta",
				docs: []*document.Document{
					compatDocument("b-0", "Beta zero", map[string]any{
						knowledgesource.MetaFileName:   "beta.md",
						knowledgesource.MetaChunkIndex: float64(2),
					}),
					compatDocument("b-1", "Beta one", nil),
				},
			},
		},
		embedder:      &legacyEmbedder{recorder: sequentialRecorder},
		store:         sequentialStore,
		sourceWorkers: 1,
		docWorkers:    1,
		progressStep:  2,
	}))

	concurrentRecorder := &scenarioRecorder{}
	concurrentStore := newLegacyVectorStore(concurrentRecorder)
	traces = append(traces, runKnowledgeLoad(knowledgeLoadCase{
		name: "concurrent-success-canonicalized",
		sources: []*legacySource{
			{
				recorder: concurrentRecorder,
				name:     "concurrent-a",
				docs: []*document.Document{
					compatDocument("ca-0", "Concurrent A0", nil),
					compatDocument("ca-1", "Concurrent A1", nil),
					compatDocument("ca-2", "Concurrent A2", nil),
				},
			},
			{
				recorder: concurrentRecorder,
				name:     "concurrent-b",
				docs: []*document.Document{
					compatDocument("cb-0", "Concurrent B0", nil),
					compatDocument("cb-1", "Concurrent B1", nil),
					compatDocument("cb-2", "Concurrent B2", nil),
				},
			},
		},
		embedder:      &legacyEmbedder{recorder: concurrentRecorder},
		store:         concurrentStore,
		sourceWorkers: 2,
		docWorkers:    3,
		progressStep:  1,
		canonicalize:  true,
	}))

	embedFailure := errors.New("legacy embed failure")
	embedFailureRecorder := &scenarioRecorder{}
	embedFailureStore := newLegacyVectorStore(embedFailureRecorder)
	traces = append(traces, runKnowledgeLoad(knowledgeLoadCase{
		name: "sequential-embedder-failure-partial-write",
		sources: []*legacySource{{
			recorder: embedFailureRecorder,
			name:     "embed-failure",
			docs: []*document.Document{
				compatDocument("ef-0", "Before failure", nil),
				compatDocumentWithEmbeddingText("ef-1", "Failing document", "FAIL::embedding"),
				compatDocument("ef-2", "After failure", nil),
			},
		}},
		embedder: &legacyEmbedder{
			recorder: embedFailureRecorder,
			failText: "FAIL::embedding",
			failErr:  embedFailure,
		},
		store:         embedFailureStore,
		sourceWorkers: 1,
		docWorkers:    1,
		progressStep:  1,
		cause:         embedFailure,
	}))

	storeFailure := errors.New("legacy store failure")
	storeFailureRecorder := &scenarioRecorder{}
	storeFailureStore := newLegacyVectorStore(storeFailureRecorder)
	storeFailureStore.failID = "sf-1"
	storeFailureStore.failErr = storeFailure
	traces = append(traces, runKnowledgeLoad(knowledgeLoadCase{
		name: "sequential-vector-store-failure-partial-write",
		sources: []*legacySource{{
			recorder: storeFailureRecorder,
			name:     "store-failure",
			docs: []*document.Document{
				compatDocument("sf-0", "Stored", nil),
				compatDocument("sf-1", "Rejected", nil),
				compatDocument("sf-2", "Not reached", nil),
			},
		}},
		embedder:      &legacyEmbedder{recorder: storeFailureRecorder},
		store:         storeFailureStore,
		sourceWorkers: 1,
		docWorkers:    1,
		progressStep:  1,
		cause:         storeFailure,
	}))

	readFailure := errors.New("legacy source read failure")
	readFailureRecorder := &scenarioRecorder{}
	readFailureStore := newLegacyVectorStore(readFailureRecorder)
	traces = append(traces, runKnowledgeLoad(knowledgeLoadCase{
		name: "sequential-source-read-failure-after-success",
		sources: []*legacySource{
			{
				recorder: readFailureRecorder,
				name:     "read-ok",
				docs:     []*document.Document{compatDocument("ro-0", "Read OK", nil)},
			},
			{
				recorder: readFailureRecorder,
				name:     "read-failure",
				err:      readFailure,
			},
		},
		embedder:      &legacyEmbedder{recorder: readFailureRecorder},
		store:         readFailureStore,
		sourceWorkers: 1,
		docWorkers:    1,
		progressStep:  1,
		cause:         readFailure,
	}))

	remoteEmbeddingRecorder := &scenarioRecorder{}
	remoteEmbeddingStore := newLegacyVectorStore(remoteEmbeddingRecorder)
	traces = append(traces, runKnowledgeLoad(knowledgeLoadCase{
		name: "nil-embedder-remote-vector-store-fallback",
		sources: []*legacySource{{
			recorder: remoteEmbeddingRecorder,
			name:     "remote-embedding",
			docs: []*document.Document{
				compatDocument("re-0", "Remote zero", nil),
				compatDocument("re-1", "Remote one", nil),
			},
		}},
		store:         remoteEmbeddingStore,
		sourceWorkers: 1,
		docWorkers:    1,
		progressStep:  1,
	}))

	syncRecorder := &scenarioRecorder{}
	syncStore := newLegacyVectorStore(syncRecorder)
	syncStore.preload(compatDocument("legacy-orphan", "Orphan", map[string]any{
		knowledgesource.MetaSourceName: "sync-source",
		knowledgesource.MetaURI:        "file://orphan",
		knowledgesource.MetaChunkIndex: 0,
	}), []float64{99, 99})
	traces = append(traces, runKnowledgeLoad(knowledgeLoadCase{
		name: "source-sync-new-document-and-orphan-cleanup",
		sources: []*legacySource{{
			recorder: syncRecorder,
			name:     "sync-source",
			metadata: map[string]any{"revision": 7, "tenant": "blue"},
			docs: []*document.Document{compatDocument("reader-id-is-replaced", "Fresh sync content", map[string]any{
				knowledgesource.MetaURI:        "file://fresh",
				knowledgesource.MetaChunkIndex: 0,
			})},
		}},
		embedder:      &legacyEmbedder{recorder: syncRecorder},
		store:         syncStore,
		enableSync:    true,
		sourceWorkers: 1,
		docWorkers:    1,
		progressStep:  1,
	}))

	return traces
}

func runOpenAIWireScenario(
	name string,
	model string,
	explicitDimensions bool,
) openAITrace {
	var (
		mu       sync.Mutex
		requests []httpRequestTrace
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requestTrace, err := captureHTTPRequest(request)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, requestTrace)
		mu.Unlock()
		writeEmbeddingResponse(w, []float64{11, 22}, 3, 3)
	}))
	defer server.Close()

	options := []openaiembedder.Option{
		openaiembedder.WithBaseURL(server.URL),
		openaiembedder.WithAPIKey("legacy-key"),
		openaiembedder.WithModel(model),
		openaiembedder.WithEncodingFormat(openaiembedder.EncodingFormatFloat),
		openaiembedder.WithUser("legacy-user"),
		openaiembedder.WithRequestOptions(openaioption.WithHeader("X-Legacy-Compat", "present")),
		openaiembedder.WithMaxRetries(0),
	}
	if explicitDimensions {
		options = append(options, openaiembedder.WithDimensions(2))
	}
	embedder := openaiembedder.New(options...)
	vector, err := embedder.GetEmbedding(context.Background(), "wire text")
	mu.Lock()
	defer mu.Unlock()
	return openAITrace{
		Name:     name,
		Requests: append([]httpRequestTrace(nil), requests...),
		Vector:   vector,
		Error:    errorText(err),
		Attempts: int64(len(requests)),
	}
}

func runOpenAIUsageScenario() openAITrace {
	var (
		mu       sync.Mutex
		requests []httpRequestTrace
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requestTrace, err := captureHTTPRequest(request)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, requestTrace)
		mu.Unlock()
		writeEmbeddingResponse(w, []float64{7, 8}, 5, 6)
	}))
	defer server.Close()

	embedder := openaiembedder.New(
		openaiembedder.WithBaseURL(server.URL),
		openaiembedder.WithAPIKey("legacy-key"),
		openaiembedder.WithModel("usage-model"),
		openaiembedder.WithDimensions(2),
		openaiembedder.WithMaxRetries(0),
	)
	vector, usage, err := embedder.GetEmbeddingWithUsage(context.Background(), "usage text")
	mu.Lock()
	defer mu.Unlock()
	return openAITrace{
		Name:     "single-with-usage",
		Requests: append([]httpRequestTrace(nil), requests...),
		Vector:   vector,
		Usage:    canonicalJSON(usage),
		Error:    errorText(err),
		Attempts: int64(len(requests)),
	}
}

func runOpenAIRetryScenario() openAITrace {
	var (
		mu       sync.Mutex
		requests []httpRequestTrace
		attempts atomic.Int64
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requestTrace, err := captureHTTPRequest(request)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, requestTrace)
		mu.Unlock()
		switch attempts.Add(1) {
		case 1:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"message": "retry transport failure"},
			})
		case 2:
			writeEmbeddingResponse(w, []float64{}, 0, 0)
		default:
			writeEmbeddingResponse(w, []float64{31, 32}, 4, 4)
		}
	}))
	defer server.Close()

	embedder := openaiembedder.New(
		openaiembedder.WithBaseURL(server.URL),
		openaiembedder.WithAPIKey("legacy-key"),
		openaiembedder.WithModel("retry-model"),
		openaiembedder.WithDimensions(2),
		openaiembedder.WithMaxRetries(2),
		openaiembedder.WithRetryBackoff([]time.Duration{0}),
	)
	vector, err := embedder.GetEmbedding(context.Background(), "retry text")
	mu.Lock()
	defer mu.Unlock()
	return openAITrace{
		Name:     "single-retry-http-then-invalid-response",
		Requests: append([]httpRequestTrace(nil), requests...),
		Vector:   vector,
		Error:    errorText(err),
		Attempts: attempts.Load(),
	}
}

func runOpenAICancellationScenario() openAITrace {
	var (
		mu        sync.Mutex
		requests  []httpRequestTrace
		attempts  atomic.Int64
		responded = make(chan struct{})
		once      sync.Once
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requestTrace, err := captureHTTPRequest(request)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, requestTrace)
		mu.Unlock()
		attempts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "cancel during retry backoff"},
		})
		once.Do(func() { close(responded) })
	}))
	defer server.Close()

	embedder := openaiembedder.New(
		openaiembedder.WithBaseURL(server.URL),
		openaiembedder.WithAPIKey("legacy-key"),
		openaiembedder.WithModel("cancel-model"),
		openaiembedder.WithDimensions(2),
		openaiembedder.WithMaxRetries(3),
		openaiembedder.WithRetryBackoff([]time.Duration{time.Hour}),
	)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := embedder.GetEmbedding(ctx, "cancel text")
		result <- err
	}()

	timedOut := false
	select {
	case <-responded:
	case <-time.After(3 * time.Second):
		timedOut = true
	}
	cancel()
	var err error
	select {
	case err = <-result:
	case <-time.After(3 * time.Second):
		err = errors.New("timed out waiting for cancellation result")
	}
	if timedOut {
		err = fmt.Errorf("timed out waiting for cancellation probe response: %w", err)
	}
	mu.Lock()
	defer mu.Unlock()
	return openAITrace{
		Name:         "single-cancel-during-retry-backoff",
		Requests:     append([]httpRequestTrace(nil), requests...),
		Error:        errorText(err),
		CauseMatched: errors.Is(err, context.Canceled),
		Attempts:     attempts.Load(),
	}
}

func runOpenAIEmptyInputScenario() openAITrace {
	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		writeEmbeddingResponse(w, []float64{1}, 1, 1)
	}))
	defer server.Close()
	embedder := openaiembedder.New(
		openaiembedder.WithBaseURL(server.URL),
		openaiembedder.WithAPIKey("legacy-key"),
		openaiembedder.WithMaxRetries(0),
	)
	_, err := embedder.GetEmbedding(context.Background(), "")
	return openAITrace{
		Name:     "single-empty-input-validation",
		Error:    errorText(err),
		Attempts: attempts.Load(),
	}
}

func buildOpenAITrace() []openAITrace {
	return []openAITrace{
		runOpenAIWireScenario("single-non-v3-explicit-dimensions", "bge-m3", true),
		runOpenAIWireScenario("single-non-v3-implicit-dimensions", "bge-m3", false),
		runOpenAIWireScenario(
			"single-text-embedding-3-default-dimensions",
			openaiembedder.ModelTextEmbedding3Small,
			false,
		),
		runOpenAIUsageScenario(),
		runOpenAIRetryScenario(),
		runOpenAICancellationScenario(),
		runOpenAIEmptyInputScenario(),
	}
}

func captureHTTPRequest(request *http.Request) (httpRequestTrace, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return httpRequestTrace{}, err
	}
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return httpRequestTrace{}, err
	}
	return httpRequestTrace{
		Path:          request.URL.Path,
		Authorization: request.Header.Get("Authorization"),
		ContentType:   request.Header.Get("Content-Type"),
		CustomHeader:  request.Header.Get("X-Legacy-Compat"),
		Body:          canonicalJSON(decoded),
	}, nil
}

func writeEmbeddingResponse(
	w http.ResponseWriter,
	embedding []float64,
	promptTokens int,
	totalTokens int,
) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data": []map[string]any{{
			"object":    "embedding",
			"index":     0,
			"embedding": embedding,
		}},
		"model": "legacy-compat",
		"usage": map[string]any{
			"prompt_tokens": promptTokens,
			"total_tokens":  totalTokens,
		},
	})
}

func compatDocument(id string, content string, metadata map[string]any) *document.Document {
	return &document.Document{
		ID:       id,
		Name:     "name-" + id,
		Content:  content,
		Metadata: metadata,
	}
}

func compatDocumentWithEmbeddingText(id string, content string, embeddingText string) *document.Document {
	doc := compatDocument(id, content, map[string]any{"custom": true})
	doc.EmbeddingText = embeddingText
	return doc
}

func vectorForText(text string) []float64 {
	checksum := 0
	for _, r := range text {
		checksum += int(r)
	}
	return []float64{float64(len([]rune(text))), float64(checksum)}
}

func metadataMatches(metadata map[string]any, filter map[string]any) bool {
	for key, value := range filter {
		if !reflect.DeepEqual(metadata[key], value) {
			return false
		}
	}
	return true
}

func canonicalJSON(value any) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func errorText(err error) string {
	if err == nil {
		return "ok"
	}
	return err.Error()
}

func TestPR2459LegacyCompatibilityTrace(t *testing.T) {
	trace := compatibilityTrace{
		Knowledge: buildKnowledgeTrace(),
		OpenAI:    buildOpenAITrace(),
	}
	payload, err := json.Marshal(trace)
	if err != nil {
		t.Fatalf("marshal compatibility trace: %v", err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	t.Logf("PR2459_LEGACY_TRACE_SHA256=%s", digest)
	if legacyCompatibilityGoldenSHA256 == "" {
		t.Logf("PR2459_LEGACY_TRACE=%s", strings.TrimSpace(string(payload)))
		return
	}
	if digest != legacyCompatibilityGoldenSHA256 {
		t.Fatalf("legacy compatibility trace digest = %s, want base digest %s\ntrace=%s",
			digest, legacyCompatibilityGoldenSHA256, payload)
	}
}
