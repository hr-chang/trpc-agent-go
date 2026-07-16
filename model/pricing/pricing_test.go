//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package pricing

import (
	"math"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

func TestIncludedCachedInputUsageAndCalculate(t *testing.T) {
	usage, err := IncludedCachedInputUsage(&model.Usage{
		PromptTokens: 206203477, CompletionTokens: 3600312,
		PromptTokensDetails: model.PromptTokensDetails{CachedTokens: 201521216},
	})
	if err != nil {
		t.Fatal(err)
	}
	estimate, err := Calculate(RateCard{
		Currency: "CNY", UncachedInput: 8, CachedInput: 2, Output: 28,
	}, usage)
	if err != nil {
		t.Fatal(err)
	}
	if estimate.Usage.UncachedInput != 4682261 || estimate.Usage.CachedInput != 201521216 {
		t.Fatalf("usage = %+v", estimate.Usage)
	}
	if math.Abs(estimate.TotalCost-541.309256) > 0.0000001 {
		t.Fatalf("total cost = %.9f", estimate.TotalCost)
	}
}

func TestSeparateCacheUsageDoesNotDoubleCountCacheRead(t *testing.T) {
	usage, err := SeparateCacheUsage(&model.Usage{
		PromptTokens: 100, CompletionTokens: 20,
		PromptTokensDetails: model.PromptTokensDetails{
			CachedTokens: 80, CacheReadTokens: 80, CacheCreationTokens: 10,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if usage.UncachedInput != 100 || usage.CachedInput != 0 ||
		usage.CacheReadInput != 80 || usage.CacheCreationInput != 10 || usage.Output != 20 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestIncludedCachedInputUsageRejectsImpossibleBreakdown(t *testing.T) {
	_, err := IncludedCachedInputUsage(&model.Usage{
		PromptTokens: 10, PromptTokensDetails: model.PromptTokensDetails{CachedTokens: 11},
	})
	if err == nil {
		t.Fatal("expected cached-token validation error")
	}
}
