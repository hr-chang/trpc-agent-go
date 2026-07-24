//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

// Package toolorder provides deterministic ordering helpers for model tools.
package toolorder

import (
	"sort"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// SortedTools returns tools in deterministic request order.
//
// Model adapters and telemetry both use this helper so the exported tool
// definitions match the order sent to model providers.
func SortedTools(tools map[string]tool.Tool) []tool.Tool {
	names := make([]string, 0, len(tools))
	for name, t := range tools {
		if t == nil || t.Declaration() == nil {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	result := make([]tool.Tool, 0, len(names))
	for _, name := range names {
		result = append(result, tools[name])
	}
	return result
}

// OrderedTools returns tools in deterministic request order, with preferred
// map keys first. Unknown and duplicate preferred keys are ignored, and tools
// not named in preferred retain the default alphabetical order.
//
// An empty preferred list preserves SortedTools behavior exactly.
func OrderedTools(tools map[string]tool.Tool, preferred []string) []tool.Tool {
	if len(preferred) == 0 {
		return SortedTools(tools)
	}

	result := make([]tool.Tool, 0, len(tools))
	seen := make(map[string]struct{}, len(preferred))
	for _, name := range preferred {
		if _, ok := seen[name]; ok {
			continue
		}
		t, ok := tools[name]
		if !ok || t == nil || t.Declaration() == nil {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, t)
	}

	remainingNames := make([]string, 0, len(tools)-len(result))
	for name, t := range tools {
		if _, ok := seen[name]; ok {
			continue
		}
		if t == nil || t.Declaration() == nil {
			continue
		}
		remainingNames = append(remainingNames, name)
	}
	sort.Strings(remainingNames)
	for _, name := range remainingNames {
		result = append(result, tools[name])
	}
	return result
}
