//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

package mongodb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithClientBuilderURI(t *testing.T) {
	opts := &ClientBuilderOpts{}

	WithClientBuilderURI("mongodb://localhost:27017")(opts)
	assert.Equal(t, "mongodb://localhost:27017", opts.URI)

	// Test overwrite
	WithClientBuilderURI("mongodb://otherhost:27017")(opts)
	assert.Equal(t, "mongodb://otherhost:27017", opts.URI)
}

func TestWithExtraOptions(t *testing.T) {
	opts := &ClientBuilderOpts{}

	WithExtraOptions("opt1", "opt2")(opts)
	assert.Len(t, opts.ExtraOptions, 2)
	assert.Equal(t, "opt1", opts.ExtraOptions[0])
	assert.Equal(t, "opt2", opts.ExtraOptions[1])
}

func TestWithExtraOptions_Append(t *testing.T) {
	opts := &ClientBuilderOpts{}

	WithExtraOptions("opt1")(opts)
	WithExtraOptions("opt2", "opt3")(opts)
	assert.Equal(t, []any{"opt1", "opt2", "opt3"}, opts.ExtraOptions)
}

func TestClientBuilderOptsDefaults(t *testing.T) {
	opts := &ClientBuilderOpts{}
	assert.Empty(t, opts.URI)
	assert.Nil(t, opts.ExtraOptions)
}

func TestWithExtraOptions_Empty(t *testing.T) {
	opts := &ClientBuilderOpts{}
	WithExtraOptions()(opts)
	assert.Empty(t, opts.ExtraOptions)
}

func TestWithExtraOptions_MixedTypes(t *testing.T) {
	opts := &ClientBuilderOpts{}

	type custom struct{ Name string }
	WithExtraOptions("string", 123, true, custom{"test"})(opts)
	require.Equal(t, 4, len(opts.ExtraOptions))
	assert.Equal(t, "string", opts.ExtraOptions[0])
	assert.Equal(t, 123, opts.ExtraOptions[1])
	assert.Equal(t, true, opts.ExtraOptions[2])
	assert.Equal(t, custom{"test"}, opts.ExtraOptions[3])
}
