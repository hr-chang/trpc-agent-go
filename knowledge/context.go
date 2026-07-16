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
	"fmt"
	"strings"
	"unicode/utf8"

	"trpc.group/trpc-go/trpc-agent-go/knowledge/source"
)

const (
	defaultContextMaxResults = 4
	defaultContextMaxChars   = 6000
)

// ContextRequest configures retrieval and bounded rendering for proactive RAG
// context. It is useful when an agent should receive relevant knowledge before
// deciding whether to call a search tool.
type ContextRequest struct {
	Query      string
	MaxResults int
	MaxChars   int
	MinScore   float64
	SearchMode int
}

// ContextResult is a compact model-facing rendering of retrieved documents.
type ContextResult struct {
	Text      string
	Documents int
	Chars     int
}

// BuildContext retrieves and renders bounded context from a knowledge base.
func BuildContext(ctx context.Context, kb Knowledge, req *ContextRequest) (*ContextResult, error) {
	if kb == nil {
		return nil, fmt.Errorf("knowledge cannot be nil")
	}
	if req == nil || strings.TrimSpace(req.Query) == "" {
		return nil, fmt.Errorf("context query cannot be empty")
	}
	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = defaultContextMaxResults
	}
	maxChars := req.MaxChars
	if maxChars <= 0 {
		maxChars = defaultContextMaxChars
	}
	searchResult, err := kb.Search(ctx, &SearchRequest{
		Query:      req.Query,
		MaxResults: maxResults,
		MinScore:   req.MinScore,
		SearchMode: req.SearchMode,
	})
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	chars := 0
	count := 0
	for _, item := range searchResult.Documents {
		if item == nil || item.Document == nil || strings.TrimSpace(item.Document.Content) == "" {
			continue
		}
		header := contextDocumentHeader(count+1, item)
		remaining := maxChars - chars
		if remaining <= utf8.RuneCountInString(header) {
			break
		}
		b.WriteString(header)
		chars += utf8.RuneCountInString(header)
		remaining = maxChars - chars
		content := truncateRunes(strings.TrimSpace(item.Document.Content), remaining)
		b.WriteString(content)
		chars += utf8.RuneCountInString(content)
		if chars < maxChars {
			b.WriteByte('\n')
			chars++
		}
		count++
		if chars >= maxChars {
			break
		}
	}
	text := strings.TrimSpace(b.String())
	return &ContextResult{Text: text, Documents: count, Chars: len([]rune(text))}, nil
}

func contextDocumentHeader(index int, item *Result) string {
	parts := []string{fmt.Sprintf("result=%d", index), fmt.Sprintf("score=%.4f", item.Score)}
	metadata := item.Document.Metadata
	for _, key := range []string{source.MetaFilePath, "trpc_ast_full_name", "trpc_ast_signature"} {
		if value, ok := metadata[key].(string); ok && strings.TrimSpace(value) != "" {
			parts = append(parts, key+"="+strings.TrimSpace(value))
		}
	}
	return "\n--- " + strings.Join(parts, " ") + " ---\n"
}

func truncateRunes(value string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}
