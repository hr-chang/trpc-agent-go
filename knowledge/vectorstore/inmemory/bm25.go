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
	"math"
	"strings"
	"unicode"

	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore"
)

const (
	bm25K1 = 1.2
	bm25B  = 0.75
	rrfK   = 60.0
)

var bm25MetadataKeys = []string{
	"trpc_ast_name",
	"trpc_ast_full_name",
	"trpc_ast_package",
	"trpc_ast_file_path",
	"trpc_ast_signature",
	"trpc_ast_comment",
}

type bm25Document struct {
	termFrequency map[string]int
	length        int
}

type bm25Index struct {
	documents           map[string]bm25Document
	documentFrequency   map[string]int
	totalDocumentLength int
}

func newBM25Index() *bm25Index {
	return &bm25Index{
		documents:         make(map[string]bm25Document),
		documentFrequency: make(map[string]int),
	}
}

func (i *bm25Index) upsert(id, text string) {
	i.delete(id)
	tokens := codeTokens(text)
	doc := bm25Document{termFrequency: make(map[string]int), length: len(tokens)}
	for _, token := range tokens {
		doc.termFrequency[token]++
	}
	for term := range doc.termFrequency {
		i.documentFrequency[term]++
	}
	i.documents[id] = doc
	i.totalDocumentLength += doc.length
}

func (i *bm25Index) delete(id string) {
	doc, ok := i.documents[id]
	if !ok {
		return
	}
	for term := range doc.termFrequency {
		i.documentFrequency[term]--
		if i.documentFrequency[term] <= 0 {
			delete(i.documentFrequency, term)
		}
	}
	i.totalDocumentLength -= doc.length
	delete(i.documents, id)
}

func (i *bm25Index) scores(query string) map[string]float64 {
	queryTerms := uniqueStrings(codeTokens(query))
	result := make(map[string]float64)
	if len(queryTerms) == 0 || len(i.documents) == 0 {
		return result
	}
	n := float64(len(i.documents))
	avgLength := float64(i.totalDocumentLength) / n
	if avgLength == 0 {
		avgLength = 1
	}
	for id, doc := range i.documents {
		var score float64
		for _, term := range queryTerms {
			tf := float64(doc.termFrequency[term])
			if tf == 0 {
				continue
			}
			df := float64(i.documentFrequency[term])
			idf := math.Log(1 + (n-df+0.5)/(df+0.5))
			denominator := tf + bm25K1*(1-bm25B+bm25B*float64(doc.length)/avgLength)
			score += idf * tf * (bm25K1 + 1) / denominator
		}
		if score > 0 {
			result[id] = score
		}
	}
	return result
}

func bm25DocumentText(doc *document.Document) string {
	if doc == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(doc.Content)
	for _, key := range bm25MetadataKeys {
		value, ok := doc.Metadata[key]
		if !ok {
			continue
		}
		if text, ok := value.(string); ok && text != "" {
			b.WriteByte('\n')
			b.WriteString(text)
		}
	}
	return b.String()
}

// codeTokens keeps full code identifiers and paths while also emitting their
// snake-case, dotted-path, slash-path, and camel-case components.
func codeTokens(text string) []string {
	var tokens []string
	flush := func(raw []rune) {
		if len(raw) == 0 {
			return
		}
		whole := strings.ToLower(string(raw))
		tokens = append(tokens, whole)
		for _, part := range strings.FieldsFunc(string(raw), func(r rune) bool {
			return r == '_' || r == '.' || r == '/' || r == '-' || r == ':'
		}) {
			tokens = append(tokens, splitCamelToken(part)...)
		}
	}

	var current []rune
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.' || r == '/' || r == '-' || r == ':' {
			current = append(current, r)
			continue
		}
		flush(current)
		current = current[:0]
	}
	flush(current)
	return compactTokens(tokens)
}

func splitCamelToken(token string) []string {
	if token == "" {
		return nil
	}
	runes := []rune(token)
	start := 0
	var result []string
	for idx := 1; idx < len(runes); idx++ {
		boundary := unicode.IsUpper(runes[idx]) && unicode.IsLower(runes[idx-1])
		acronymBoundary := idx+1 < len(runes) && unicode.IsUpper(runes[idx]) &&
			unicode.IsUpper(runes[idx-1]) && unicode.IsLower(runes[idx+1])
		if boundary || acronymBoundary {
			result = append(result, strings.ToLower(string(runes[start:idx])))
			start = idx
		}
	}
	result = append(result, strings.ToLower(string(runes[start:])))
	return result
}

func compactTokens(tokens []string) []string {
	result := tokens[:0]
	for _, token := range tokens {
		if token == "" {
			continue
		}
		result = append(result, token)
	}
	return result
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (vs *VectorStore) searchByKeyword(_ context.Context, query *vectorstore.SearchQuery) (*vectorstore.SearchResult, error) {
	vs.mutex.RLock()
	defer vs.mutex.RUnlock()

	scores := vs.bm25.scores(query.Query)
	results := vs.scoredDocuments(scores, query, true)
	return &vectorstore.SearchResult{Results: limitResults(results, vs.getMaxResult(query.Limit))}, nil
}

func (vs *VectorStore) searchHybrid(_ context.Context, query *vectorstore.SearchQuery) (*vectorstore.SearchResult, error) {
	vs.mutex.RLock()
	defer vs.mutex.RUnlock()

	keywordScores := vs.bm25.scores(query.Query)
	keywordRanking := vs.scoredDocuments(keywordScores, query, false)
	var vectorRanking []*vectorstore.ScoredDocument
	if len(query.Vector) > 0 {
		vectorScores := make(map[string]float64)
		for id, embedding := range vs.embeddings {
			if len(embedding) != len(query.Vector) || !vs.matchesQueryFilter(id, query) {
				continue
			}
			vectorScores[id] = cosineSimilarity(query.Vector, embedding)
		}
		vectorRanking = vs.scoredDocuments(vectorScores, query, false)
	}

	fused := reciprocalRankFusion(keywordRanking, vectorRanking)
	results := make([]*vectorstore.ScoredDocument, 0, len(fused))
	for id, score := range fused {
		if score < query.MinScore {
			continue
		}
		results = append(results, &vectorstore.ScoredDocument{Document: vs.documents[id].Clone(), Score: score})
	}
	sortByScore(results)
	return &vectorstore.SearchResult{Results: limitResults(results, vs.getMaxResult(query.Limit))}, nil
}

func (vs *VectorStore) scoredDocuments(scores map[string]float64, query *vectorstore.SearchQuery, applyMinScore bool) []*vectorstore.ScoredDocument {
	results := make([]*vectorstore.ScoredDocument, 0, len(scores))
	for id, score := range scores {
		if !vs.matchesQueryFilter(id, query) || applyMinScore && score < query.MinScore {
			continue
		}
		results = append(results, &vectorstore.ScoredDocument{Document: vs.documents[id].Clone(), Score: score})
	}
	sortByScore(results)
	return results
}

func (vs *VectorStore) matchesQueryFilter(id string, query *vectorstore.SearchQuery) bool {
	return query.Filter == nil || vs.matchesFilter(id, query.Filter)
}

func reciprocalRankFusion(rankings ...[]*vectorstore.ScoredDocument) map[string]float64 {
	result := make(map[string]float64)
	nonEmpty := 0
	for _, ranking := range rankings {
		if len(ranking) > 0 {
			nonEmpty++
		}
		for idx, item := range ranking {
			result[item.Document.ID] += 1 / (rrfK + float64(idx+1))
		}
	}
	if nonEmpty == 0 {
		return result
	}
	maxScore := float64(nonEmpty) / (rrfK + 1)
	for id := range result {
		result[id] /= maxScore
	}
	return result
}

func limitResults(results []*vectorstore.ScoredDocument, limit int) []*vectorstore.ScoredDocument {
	if len(results) > limit {
		return results[:limit]
	}
	return results
}
