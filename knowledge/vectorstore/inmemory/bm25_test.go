//
// Tencent is pleased to support the open source community by making
// trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package inmemory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore"
)

func TestBM25KeywordSearchWithoutEmbeddings(t *testing.T) {
	store := New(WithBM25(true))
	docs := []*document.Document{
		{ID: "timeout", Content: "func requestTimeoutHandler() error { return context.DeadlineExceeded }", Metadata: map[string]any{"trpc_ast_file_path": "client/timeout.go"}},
		{ID: "cache", Content: "func refreshCache() { cache.Store() }", Metadata: map[string]any{"trpc_ast_file_path": "cache/store.go"}},
	}
	for _, doc := range docs {
		require.NoError(t, store.Add(context.Background(), doc, nil))
	}

	result, err := store.Search(context.Background(), &vectorstore.SearchQuery{
		Query:      "request timeout deadline",
		Limit:      5,
		SearchMode: vectorstore.SearchModeKeyword,
	})
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	require.Equal(t, "timeout", result.Results[0].Document.ID)
}

func TestBM25IndexesCodeMetadataAndIdentifierParts(t *testing.T) {
	store := New(WithBM25(true))
	require.NoError(t, store.Add(context.Background(), &document.Document{
		ID:      "doc",
		Content: "return err",
		Metadata: map[string]any{
			"trpc_ast_full_name": "transport.HTTPRequestTimeoutHandler",
			"trpc_ast_file_path": "internal/http/request_timeout.go",
		},
	}, nil))

	for _, query := range []string{"HTTPRequest", "request_timeout", "internal/http"} {
		result, err := store.Search(context.Background(), &vectorstore.SearchQuery{
			Query: query, Limit: 1, SearchMode: vectorstore.SearchModeKeyword,
		})
		require.NoError(t, err)
		require.Len(t, result.Results, 1, query)
	}
}

func TestBM25HybridFallsBackToKeywordWithoutVector(t *testing.T) {
	store := New(WithBM25(true))
	require.NoError(t, store.Add(context.Background(), &document.Document{ID: "a", Content: "parse request timeout"}, nil))

	result, err := store.Search(context.Background(), &vectorstore.SearchQuery{
		Query: "timeout", Limit: 1, SearchMode: vectorstore.SearchModeHybrid,
	})
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	require.Equal(t, "a", result.Results[0].Document.ID)
}

func TestBM25DeleteRemovesLexicalDocument(t *testing.T) {
	store := New(WithBM25(true))
	require.NoError(t, store.Add(context.Background(), &document.Document{ID: "a", Content: "unique timeout token"}, nil))
	require.NoError(t, store.Delete(context.Background(), "a"))

	result, err := store.Search(context.Background(), &vectorstore.SearchQuery{
		Query: "unique", Limit: 1, SearchMode: vectorstore.SearchModeKeyword,
	})
	require.NoError(t, err)
	require.Empty(t, result.Results)
}
