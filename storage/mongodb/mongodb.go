//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

// Package mongodb provides the MongoDB instance info management and client interface.
package mongodb

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func init() {
	mongodbRegistry = make(map[string][]ClientBuilderOpt)
}

var mongodbRegistry map[string][]ClientBuilderOpt

// clientBuilder is the function type for building Client instances.
type clientBuilder func(ctx context.Context, builderOpts ...ClientBuilderOpt) (Client, error)

var globalBuilder clientBuilder = defaultClientBuilder

// SetClientBuilder sets the mongodb client builder.
func SetClientBuilder(builder clientBuilder) {
	globalBuilder = builder
}

// GetClientBuilder gets the mongodb client builder.
func GetClientBuilder() clientBuilder {
	return globalBuilder
}

// mongoConnector is the function used to connect to MongoDB.
// It is overridable in tests to inject a fake connector.
var mongoConnector = func(ctx context.Context, opts ...*options.ClientOptions) (*mongo.Client, error) {
	return mongo.Connect(ctx, opts...)
}

// defaultClientBuilder is the default mongodb client builder.
// It creates a native MongoDB client using the official Go driver.
func defaultClientBuilder(ctx context.Context, builderOpts ...ClientBuilderOpt) (Client, error) {
	o := &ClientBuilderOpts{}
	for _, opt := range builderOpts {
		opt(o)
	}

	if o.URI == "" {
		return nil, errors.New("mongodb: uri is empty")
	}

	client, err := mongoConnector(ctx, options.Client().ApplyURI(o.URI))
	if err != nil {
		return nil, fmt.Errorf("mongodb: connect: %w", err)
	}

	// Verify connection.
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("mongodb: ping: %w", err)
	}

	return newDefaultClient(client), nil
}

// RegisterMongoDBInstance registers a mongodb instance with the given options.
func RegisterMongoDBInstance(name string, opts ...ClientBuilderOpt) {
	mongodbRegistry[name] = append(mongodbRegistry[name], opts...)
}

// GetMongoDBInstance gets the mongodb instance options by name.
func GetMongoDBInstance(name string) ([]ClientBuilderOpt, bool) {
	if _, ok := mongodbRegistry[name]; !ok {
		return nil, false
	}
	return mongodbRegistry[name], true
}

// Client defines the interface for MongoDB operations.
// It abstracts the common MongoDB operations needed by upstream packages
// (such as session/mongodb), making it easier to inject mock implementations
// for testing.
type Client interface {
	// InsertOne inserts a single document into the collection.
	InsertOne(ctx context.Context, database, coll string, document any,
		opts ...*options.InsertOneOptions) (*mongo.InsertOneResult, error)

	// InsertMany inserts multiple documents into the collection in a single batch.
	InsertMany(ctx context.Context, database, coll string, documents []any,
		opts ...*options.InsertManyOptions) (*mongo.InsertManyResult, error)

	// UpdateOne updates at most one document matching the filter.
	UpdateOne(ctx context.Context, database, coll string, filter, update any,
		opts ...*options.UpdateOptions) (*mongo.UpdateResult, error)

	// UpdateMany updates all documents matching the filter.
	UpdateMany(ctx context.Context, database, coll string, filter, update any,
		opts ...*options.UpdateOptions) (*mongo.UpdateResult, error)

	// DeleteOne deletes at most one document matching the filter.
	DeleteOne(ctx context.Context, database, coll string, filter any,
		opts ...*options.DeleteOptions) (*mongo.DeleteResult, error)

	// DeleteMany deletes all documents matching the filter.
	DeleteMany(ctx context.Context, database, coll string, filter any,
		opts ...*options.DeleteOptions) (*mongo.DeleteResult, error)

	// FindOne finds a single document matching the filter.
	FindOne(ctx context.Context, database, coll string, filter any,
		opts ...*options.FindOneOptions) *mongo.SingleResult

	// FindOneAndUpdate atomically finds a single document and updates it.
	FindOneAndUpdate(ctx context.Context, database, coll string, filter, update any,
		opts ...*options.FindOneAndUpdateOptions) *mongo.SingleResult

	// Find returns a cursor over documents matching the filter.
	// Callers must close the returned cursor when done.
	Find(ctx context.Context, database, coll string, filter any,
		opts ...*options.FindOptions) (*mongo.Cursor, error)

	// Aggregate runs an aggregation pipeline on the collection and returns a cursor.
	// Callers must close the returned cursor when done.
	Aggregate(ctx context.Context, database, coll string, pipeline any,
		opts ...*options.AggregateOptions) (*mongo.Cursor, error)

	// CountDocuments returns the number of documents matching the filter.
	CountDocuments(ctx context.Context, database, coll string, filter any,
		opts ...*options.CountOptions) (int64, error)

	// EnsureIndexes creates the given indexes on the collection if they do not exist.
	// Index creation is idempotent: existing indexes with matching keys and options
	// are left unchanged.
	EnsureIndexes(ctx context.Context, database, coll string,
		models []mongo.IndexModel, opts ...*options.CreateIndexesOptions) ([]string, error)

	// Transaction executes fn within a multi-document transaction.
	// Note: MongoDB transactions require a replica set or sharded cluster deployment;
	// they are not supported on standalone servers.
	Transaction(ctx context.Context, fn TxFunc, opts ...TxOption) error

	// Close terminates all connections to the MongoDB deployment.
	// After calling Close, the client should not be used anymore.
	Close(ctx context.Context) error
}

// TxFunc is a user transaction function.
// Return nil to commit, or any error to rollback.
type TxFunc func(sc mongo.SessionContext) error

// TxOption configures transaction options.
type TxOption func(*TxOptions)

// TxOptions are the configurable options of a transaction.
type TxOptions struct {
	// Transaction holds the per-transaction options. May be nil.
	Transaction *options.TransactionOptions
	// Session holds the per-session options. May be nil.
	Session *options.SessionOptions
}

// WithTransactionOptions sets the per-transaction options.
func WithTransactionOptions(o *options.TransactionOptions) TxOption {
	return func(opts *TxOptions) {
		opts.Transaction = o
	}
}

// WithSessionOptions sets the per-session options.
func WithSessionOptions(o *options.SessionOptions) TxOption {
	return func(opts *TxOptions) {
		opts.Session = o
	}
}

// session is the subset of *mongo.Session used by defaultClient.
// Defining it as an interface lets tests inject a fake session without
// connecting to a real MongoDB deployment.
type session interface {
	EndSession(ctx context.Context)
	WithTransaction(ctx context.Context, fn func(sc mongo.SessionContext) (any, error),
		opts ...*options.TransactionOptions) (any, error)
}

// defaultClient wraps *mongo.Client to implement the Client interface.
type defaultClient struct {
	client       *mongo.Client
	startSession func(opts ...*options.SessionOptions) (session, error)
}

// newDefaultClient creates a new defaultClient with the given mongo.Client.
func newDefaultClient(client *mongo.Client) *defaultClient {
	return &defaultClient{
		client: client,
		startSession: func(opts ...*options.SessionOptions) (session, error) {
			return client.StartSession(opts...)
		},
	}
}

func (c *defaultClient) coll(database, coll string) *mongo.Collection {
	return c.client.Database(database).Collection(coll)
}

// InsertOne implements Client.InsertOne.
func (c *defaultClient) InsertOne(ctx context.Context, database, coll string, document any,
	opts ...*options.InsertOneOptions) (*mongo.InsertOneResult, error) {
	return c.coll(database, coll).InsertOne(ctx, document, opts...)
}

// InsertMany implements Client.InsertMany.
func (c *defaultClient) InsertMany(ctx context.Context, database, coll string, documents []any,
	opts ...*options.InsertManyOptions) (*mongo.InsertManyResult, error) {
	return c.coll(database, coll).InsertMany(ctx, documents, opts...)
}

// UpdateOne implements Client.UpdateOne.
func (c *defaultClient) UpdateOne(ctx context.Context, database, coll string, filter, update any,
	opts ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
	return c.coll(database, coll).UpdateOne(ctx, filter, update, opts...)
}

// UpdateMany implements Client.UpdateMany.
func (c *defaultClient) UpdateMany(ctx context.Context, database, coll string, filter, update any,
	opts ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
	return c.coll(database, coll).UpdateMany(ctx, filter, update, opts...)
}

// DeleteOne implements Client.DeleteOne.
func (c *defaultClient) DeleteOne(ctx context.Context, database, coll string, filter any,
	opts ...*options.DeleteOptions) (*mongo.DeleteResult, error) {
	return c.coll(database, coll).DeleteOne(ctx, filter, opts...)
}

// DeleteMany implements Client.DeleteMany.
func (c *defaultClient) DeleteMany(ctx context.Context, database, coll string, filter any,
	opts ...*options.DeleteOptions) (*mongo.DeleteResult, error) {
	return c.coll(database, coll).DeleteMany(ctx, filter, opts...)
}

// FindOne implements Client.FindOne.
func (c *defaultClient) FindOne(ctx context.Context, database, coll string, filter any,
	opts ...*options.FindOneOptions) *mongo.SingleResult {
	return c.coll(database, coll).FindOne(ctx, filter, opts...)
}

// FindOneAndUpdate implements Client.FindOneAndUpdate.
func (c *defaultClient) FindOneAndUpdate(ctx context.Context, database, coll string, filter, update any,
	opts ...*options.FindOneAndUpdateOptions) *mongo.SingleResult {
	return c.coll(database, coll).FindOneAndUpdate(ctx, filter, update, opts...)
}

// Find implements Client.Find.
func (c *defaultClient) Find(ctx context.Context, database, coll string, filter any,
	opts ...*options.FindOptions) (*mongo.Cursor, error) {
	return c.coll(database, coll).Find(ctx, filter, opts...)
}

// Aggregate implements Client.Aggregate.
func (c *defaultClient) Aggregate(ctx context.Context, database, coll string, pipeline any,
	opts ...*options.AggregateOptions) (*mongo.Cursor, error) {
	return c.coll(database, coll).Aggregate(ctx, pipeline, opts...)
}

// CountDocuments implements Client.CountDocuments.
func (c *defaultClient) CountDocuments(ctx context.Context, database, coll string, filter any,
	opts ...*options.CountOptions) (int64, error) {
	return c.coll(database, coll).CountDocuments(ctx, filter, opts...)
}

// EnsureIndexes implements Client.EnsureIndexes.
func (c *defaultClient) EnsureIndexes(ctx context.Context, database, coll string,
	models []mongo.IndexModel, opts ...*options.CreateIndexesOptions) ([]string, error) {
	if len(models) == 0 {
		return nil, nil
	}
	return c.coll(database, coll).Indexes().CreateMany(ctx, models, opts...)
}

// Transaction implements Client.Transaction.
// It starts a session, executes fn inside session.WithTransaction (which handles
// commit, rollback and transient-error retries internally), and ends the session.
func (c *defaultClient) Transaction(ctx context.Context, fn TxFunc, opts ...TxOption) error {
	txOpts := &TxOptions{}
	for _, opt := range opts {
		opt(txOpts)
	}

	var sessOpts []*options.SessionOptions
	if txOpts.Session != nil {
		sessOpts = append(sessOpts, txOpts.Session)
	}

	sess, err := c.startSession(sessOpts...)
	if err != nil {
		return fmt.Errorf("mongodb: start session: %w", err)
	}
	defer sess.EndSession(ctx)

	var txOptsList []*options.TransactionOptions
	if txOpts.Transaction != nil {
		txOptsList = append(txOptsList, txOpts.Transaction)
	}

	_, err = sess.WithTransaction(ctx, func(sc mongo.SessionContext) (any, error) {
		return nil, fn(sc)
	}, txOptsList...)
	return err
}

// Close implements Client.Close.
func (c *defaultClient) Close(ctx context.Context) error {
	return c.client.Disconnect(ctx)
}
