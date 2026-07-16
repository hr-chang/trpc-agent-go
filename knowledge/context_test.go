//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package knowledge

import (
	"context"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source"
)

type contextKnowledgeStub struct {
	req *SearchRequest
}

func (s *contextKnowledgeStub) Search(_ context.Context, req *SearchRequest) (*SearchResult, error) {
	s.req = req
	return &SearchResult{Documents: []*Result{{
		Document: &document.Document{Content: strings.Repeat("code ", 100), Metadata: map[string]any{
			source.MetaFilePath: "pkg/service.py",
		}},
		Score: 0.9,
	}}}, nil
}

func TestBuildContextBoundsAndLabelsResults(t *testing.T) {
	kb := &contextKnowledgeStub{}
	result, err := BuildContext(context.Background(), kb, &ContextRequest{
		Query: "find service", MaxResults: 3, MaxChars: 120, SearchMode: 2,
	})
	if err != nil {
		t.Fatalf("BuildContext() error = %v", err)
	}
	if result.Documents != 1 || result.Chars > 120 {
		t.Fatalf("context result = %+v", result)
	}
	if !strings.Contains(result.Text, "pkg/service.py") || !strings.Contains(result.Text, "score=0.9000") {
		t.Fatalf("context text = %q", result.Text)
	}
	if kb.req.MaxResults != 3 || kb.req.SearchMode != 2 {
		t.Fatalf("search request = %+v", kb.req)
	}
}
