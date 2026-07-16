//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package repo

import "context"

// Materializer exposes a repository-like workspace as a local directory. It
// lets repository knowledge consume code that lives behind another execution
// boundary, such as a container, remote sandbox, or generated workspace.
type Materializer interface {
	Materialize(ctx context.Context) (*MaterializedRepository, error)
}

// MaterializedRepository is one local snapshot returned by a Materializer.
// StableURI should identify the logical workspace independent of the temporary
// local directory, for example workspace://case-123. It is used as the base of
// per-file document URIs so repeated snapshots retain stable document IDs.
type MaterializedRepository struct {
	Root      string
	Name      string
	URL       string
	Revision  string
	StableURI string
	Cleanup   func()
}
