//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

// Package pricing calculates model cost from normalized billable token buckets.
package pricing

import (
	"fmt"
	"math"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

const defaultUnitTokens int64 = 1_000_000

// RateCard defines prices per UnitTokens. Currency and prices are supplied by
// the caller so private deployments and negotiated prices do not need to be
// hard-coded in the framework.
type RateCard struct {
	Currency           string  `json:"currency"`
	UnitTokens         int64   `json:"unit_tokens"`
	UncachedInput      float64 `json:"uncached_input"`
	CachedInput        float64 `json:"cached_input"`
	CacheReadInput     float64 `json:"cache_read_input,omitempty"`
	CacheCreationInput float64 `json:"cache_creation_input,omitempty"`
	Output             float64 `json:"output"`
}

// TokenUsage contains non-overlapping billable token buckets. Separating this
// type from model.Usage keeps provider-specific cache accounting explicit.
type TokenUsage struct {
	UncachedInput      int64 `json:"uncached_input"`
	CachedInput        int64 `json:"cached_input"`
	CacheReadInput     int64 `json:"cache_read_input,omitempty"`
	CacheCreationInput int64 `json:"cache_creation_input,omitempty"`
	Output             int64 `json:"output"`
}

// Estimate contains the usage and contribution of every priced token bucket.
type Estimate struct {
	Currency               string     `json:"currency"`
	UnitTokens             int64      `json:"unit_tokens"`
	Usage                  TokenUsage `json:"usage"`
	UncachedInputCost      float64    `json:"uncached_input_cost"`
	CachedInputCost        float64    `json:"cached_input_cost"`
	CacheReadInputCost     float64    `json:"cache_read_input_cost,omitempty"`
	CacheCreationInputCost float64    `json:"cache_creation_input_cost,omitempty"`
	OutputCost             float64    `json:"output_cost"`
	TotalCost              float64    `json:"total_cost"`
}

// Validate checks whether a rate card can produce an unambiguous estimate.
func (r RateCard) Validate() error {
	if strings.TrimSpace(r.Currency) == "" {
		return fmt.Errorf("pricing currency cannot be empty")
	}
	if r.UnitTokens < 0 {
		return fmt.Errorf("pricing unit tokens cannot be negative")
	}
	for name, value := range map[string]float64{
		"uncached input": r.UncachedInput, "cached input": r.CachedInput,
		"cache read input": r.CacheReadInput, "cache creation input": r.CacheCreationInput,
		"output": r.Output,
	} {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("pricing %s rate must be a finite non-negative number", name)
		}
	}
	return nil
}

// IncludedCachedInputUsage converts usage from APIs where PromptTokens already
// includes CachedTokens, including OpenAI-compatible APIs and Gemini.
func IncludedCachedInputUsage(usage *model.Usage) (TokenUsage, error) {
	if usage == nil {
		return TokenUsage{}, fmt.Errorf("model usage cannot be nil")
	}
	if usage.PromptTokens < 0 || usage.CompletionTokens < 0 {
		return TokenUsage{}, fmt.Errorf("model token usage cannot be negative")
	}
	cached := usage.PromptTokensDetails.CachedTokens
	if cached < 0 || cached > usage.PromptTokens {
		return TokenUsage{}, fmt.Errorf(
			"cached input tokens %d exceed prompt tokens %d", cached, usage.PromptTokens,
		)
	}
	return TokenUsage{
		UncachedInput: int64(usage.PromptTokens - cached),
		CachedInput:   int64(cached),
		Output:        int64(usage.CompletionTokens),
	}, nil
}

// SeparateCacheUsage converts usage from APIs where PromptTokens excludes
// separately reported cache-read and cache-creation tokens, including
// Anthropic. CachedTokens may mirror CacheReadTokens and is not double-counted.
func SeparateCacheUsage(usage *model.Usage) (TokenUsage, error) {
	if usage == nil {
		return TokenUsage{}, fmt.Errorf("model usage cannot be nil")
	}
	if usage.PromptTokens < 0 || usage.CompletionTokens < 0 ||
		usage.PromptTokensDetails.CacheReadTokens < 0 ||
		usage.PromptTokensDetails.CacheCreationTokens < 0 {
		return TokenUsage{}, fmt.Errorf("model token usage cannot be negative")
	}
	cacheRead := usage.PromptTokensDetails.CacheReadTokens
	if cacheRead == 0 {
		cacheRead = usage.PromptTokensDetails.CachedTokens
	}
	return TokenUsage{
		UncachedInput:      int64(usage.PromptTokens),
		CacheReadInput:     int64(cacheRead),
		CacheCreationInput: int64(usage.PromptTokensDetails.CacheCreationTokens),
		Output:             int64(usage.CompletionTokens),
	}, nil
}

// Calculate applies a rate card to normalized, non-overlapping token usage.
func Calculate(rateCard RateCard, usage TokenUsage) (*Estimate, error) {
	if err := rateCard.Validate(); err != nil {
		return nil, err
	}
	for name, value := range map[string]int64{
		"uncached input": usage.UncachedInput, "cached input": usage.CachedInput,
		"cache read input": usage.CacheReadInput, "cache creation input": usage.CacheCreationInput,
		"output": usage.Output,
	} {
		if value < 0 {
			return nil, fmt.Errorf("%s token usage cannot be negative", name)
		}
	}
	unit := rateCard.UnitTokens
	if unit == 0 {
		unit = defaultUnitTokens
	}
	estimate := &Estimate{
		Currency:               rateCard.Currency,
		UnitTokens:             unit,
		Usage:                  usage,
		UncachedInputCost:      bucketCost(usage.UncachedInput, rateCard.UncachedInput, unit),
		CachedInputCost:        bucketCost(usage.CachedInput, rateCard.CachedInput, unit),
		CacheReadInputCost:     bucketCost(usage.CacheReadInput, rateCard.CacheReadInput, unit),
		CacheCreationInputCost: bucketCost(usage.CacheCreationInput, rateCard.CacheCreationInput, unit),
		OutputCost:             bucketCost(usage.Output, rateCard.Output, unit),
	}
	estimate.TotalCost = estimate.UncachedInputCost + estimate.CachedInputCost +
		estimate.CacheReadInputCost + estimate.CacheCreationInputCost + estimate.OutputCost
	return estimate, nil
}

func bucketCost(tokens int64, rate float64, unit int64) float64 {
	return float64(tokens) * rate / float64(unit)
}
