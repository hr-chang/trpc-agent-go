//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

package mongodb

// ClientBuilderOpt is the option for the mongodb client.
type ClientBuilderOpt func(*ClientBuilderOpts)

// ClientBuilderOpts is the options for the mongodb client.
type ClientBuilderOpts struct {
	// URI is the mongodb connection string.
	// Format: mongodb://[username:password@]host1[:port1][,...hostN[:portN]][/[defaultauthdb][?options]]
	// Example: mongodb://user:pass@localhost:27017/?replicaSet=rs0
	URI string

	// ExtraOptions is the extra options for the mongodb client.
	// This option is mainly used for customized mongodb client builders;
	// it is passed through verbatim and ignored by the default builder.
	ExtraOptions []any
}

// WithClientBuilderURI sets the mongodb connection URI for clientBuilder.
func WithClientBuilderURI(uri string) ClientBuilderOpt {
	return func(opts *ClientBuilderOpts) {
		opts.URI = uri
	}
}

// WithExtraOptions sets the mongodb client extra options for clientBuilder.
// This option is mainly used for customized mongodb client builders, it will
// be passed to the builder.
func WithExtraOptions(extraOptions ...any) ClientBuilderOpt {
	return func(opts *ClientBuilderOpts) {
		opts.ExtraOptions = append(opts.ExtraOptions, extraOptions...)
	}
}
