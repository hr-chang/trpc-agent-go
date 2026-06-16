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
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// resetRegistry resets the package-level registry before a test and restores
// it afterwards. This keeps registry tests isolated from each other.
func resetRegistry(t *testing.T) {
	t.Helper()
	old := mongodbRegistry
	mongodbRegistry = make(map[string][]ClientBuilderOpt)
	t.Cleanup(func() { mongodbRegistry = old })
}

// resetBuilder resets the package-level builder before a test and restores
// it afterwards.
func resetBuilder(t *testing.T) {
	t.Helper()
	old := globalBuilder
	t.Cleanup(func() { globalBuilder = old })
}

// resetConnector resets the package-level mongoConnector before a test and
// restores it afterwards.
func resetConnector(t *testing.T) {
	t.Helper()
	old := mongoConnector
	t.Cleanup(func() { mongoConnector = old })
}

func TestRegisterAndGetMongoDBInstance(t *testing.T) {
	resetRegistry(t)

	const (
		name = "test-instance"
		uri  = "mongodb://localhost:27017"
	)
	RegisterMongoDBInstance(name, WithClientBuilderURI(uri))

	opts, ok := GetMongoDBInstance(name)
	require.True(t, ok)
	require.Len(t, opts, 1)

	cfg := &ClientBuilderOpts{}
	for _, opt := range opts {
		opt(cfg)
	}
	assert.Equal(t, uri, cfg.URI)
}

func TestGetMongoDBInstance_NotFound(t *testing.T) {
	resetRegistry(t)

	opts, ok := GetMongoDBInstance("missing")
	assert.False(t, ok)
	assert.Nil(t, opts)
}

func TestRegisterMongoDBInstance_Append(t *testing.T) {
	resetRegistry(t)

	const name = "appendable"
	RegisterMongoDBInstance(name, WithClientBuilderURI("mongodb://localhost:27017"))
	RegisterMongoDBInstance(name, WithExtraOptions("alpha"), WithExtraOptions("beta"))

	opts, ok := GetMongoDBInstance(name)
	require.True(t, ok)
	require.Len(t, opts, 3)

	cfg := &ClientBuilderOpts{}
	for _, opt := range opts {
		opt(cfg)
	}
	assert.Equal(t, []any{"alpha", "beta"}, cfg.ExtraOptions)
}

func TestSetAndGetClientBuilder(t *testing.T) {
	resetBuilder(t)

	invoked := false
	custom := func(ctx context.Context, opts ...ClientBuilderOpt) (Client, error) {
		invoked = true
		return nil, errors.New("custom builder")
	}
	SetClientBuilder(custom)

	b := GetClientBuilder()
	require.NotNil(t, b)

	_, err := b(context.Background(), WithClientBuilderURI("mongodb://localhost:27017"))
	assert.EqualError(t, err, "custom builder")
	assert.True(t, invoked)
}

func TestDefaultClientBuilder_EmptyURI(t *testing.T) {
	_, err := defaultClientBuilder(context.Background())
	require.Error(t, err)
	assert.Equal(t, "mongodb: uri is empty", err.Error())
}

func TestDefaultClientBuilder_ConnectError(t *testing.T) {
	resetConnector(t)
	mongoConnector = func(ctx context.Context, opts ...*options.ClientOptions) (*mongo.Client, error) {
		return nil, errors.New("boom")
	}

	_, err := defaultClientBuilder(context.Background(),
		WithClientBuilderURI("mongodb://localhost:27017"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mongodb: connect")
	assert.Contains(t, err.Error(), "boom")
}

func TestDefaultClientBuilder_PingError(t *testing.T) {
	// Use a connection string that resolves quickly but cannot be reached, so
	// Ping (called inside defaultClientBuilder) fails.
	const uri = "mongodb://127.0.0.1:1/?serverSelectionTimeoutMS=50&connectTimeoutMS=50"

	_, err := defaultClientBuilder(context.Background(), WithClientBuilderURI(uri))
	require.Error(t, err)
	// Either the connect or ping path may fire first depending on driver
	// internals, but the error must be wrapped under our "mongodb:" prefix.
	assert.Contains(t, err.Error(), "mongodb:")
}

// mockClient is a manual mock of Client used for table-driven tests.
type mockClient struct {
	insertOneFn        func(ctx context.Context, db, coll string, doc any) (*mongo.InsertOneResult, error)
	insertManyFn       func(ctx context.Context, db, coll string, docs []any) (*mongo.InsertManyResult, error)
	updateOneFn        func(ctx context.Context, db, coll string, filter, update any) (*mongo.UpdateResult, error)
	updateManyFn       func(ctx context.Context, db, coll string, filter, update any) (*mongo.UpdateResult, error)
	deleteOneFn        func(ctx context.Context, db, coll string, filter any) (*mongo.DeleteResult, error)
	deleteManyFn       func(ctx context.Context, db, coll string, filter any) (*mongo.DeleteResult, error)
	findOneFn          func(ctx context.Context, db, coll string, filter any) *mongo.SingleResult
	findOneAndUpdateFn func(ctx context.Context, db, coll string, filter, update any) *mongo.SingleResult
	findFn             func(ctx context.Context, db, coll string, filter any) (*mongo.Cursor, error)
	aggregateFn        func(ctx context.Context, db, coll string, pipeline any) (*mongo.Cursor, error)
	countFn            func(ctx context.Context, db, coll string, filter any) (int64, error)
	ensureIndexesFn    func(ctx context.Context, db, coll string, models []mongo.IndexModel) ([]string, error)
	transactionFn      func(ctx context.Context, fn TxFunc, opts ...TxOption) error
	closeFn            func(ctx context.Context) error
}

func (m *mockClient) InsertOne(ctx context.Context, db, coll string, doc any,
	_ ...*options.InsertOneOptions) (*mongo.InsertOneResult, error) {
	if m.insertOneFn != nil {
		return m.insertOneFn(ctx, db, coll, doc)
	}
	return &mongo.InsertOneResult{}, nil
}

func (m *mockClient) InsertMany(ctx context.Context, db, coll string, docs []any,
	_ ...*options.InsertManyOptions) (*mongo.InsertManyResult, error) {
	if m.insertManyFn != nil {
		return m.insertManyFn(ctx, db, coll, docs)
	}
	return &mongo.InsertManyResult{}, nil
}

func (m *mockClient) UpdateOne(ctx context.Context, db, coll string, filter, update any,
	_ ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
	if m.updateOneFn != nil {
		return m.updateOneFn(ctx, db, coll, filter, update)
	}
	return &mongo.UpdateResult{}, nil
}

func (m *mockClient) UpdateMany(ctx context.Context, db, coll string, filter, update any,
	_ ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
	if m.updateManyFn != nil {
		return m.updateManyFn(ctx, db, coll, filter, update)
	}
	return &mongo.UpdateResult{}, nil
}

func (m *mockClient) DeleteOne(ctx context.Context, db, coll string, filter any,
	_ ...*options.DeleteOptions) (*mongo.DeleteResult, error) {
	if m.deleteOneFn != nil {
		return m.deleteOneFn(ctx, db, coll, filter)
	}
	return &mongo.DeleteResult{}, nil
}

func (m *mockClient) DeleteMany(ctx context.Context, db, coll string, filter any,
	_ ...*options.DeleteOptions) (*mongo.DeleteResult, error) {
	if m.deleteManyFn != nil {
		return m.deleteManyFn(ctx, db, coll, filter)
	}
	return &mongo.DeleteResult{}, nil
}

func (m *mockClient) FindOne(ctx context.Context, db, coll string, filter any,
	_ ...*options.FindOneOptions) *mongo.SingleResult {
	if m.findOneFn != nil {
		return m.findOneFn(ctx, db, coll, filter)
	}
	return nil
}

func (m *mockClient) FindOneAndUpdate(ctx context.Context, db, coll string, filter, update any,
	_ ...*options.FindOneAndUpdateOptions) *mongo.SingleResult {
	if m.findOneAndUpdateFn != nil {
		return m.findOneAndUpdateFn(ctx, db, coll, filter, update)
	}
	return nil
}

func (m *mockClient) Find(ctx context.Context, db, coll string, filter any,
	_ ...*options.FindOptions) (*mongo.Cursor, error) {
	if m.findFn != nil {
		return m.findFn(ctx, db, coll, filter)
	}
	return nil, nil
}

func (m *mockClient) Aggregate(ctx context.Context, db, coll string, pipeline any,
	_ ...*options.AggregateOptions) (*mongo.Cursor, error) {
	if m.aggregateFn != nil {
		return m.aggregateFn(ctx, db, coll, pipeline)
	}
	return nil, nil
}

func (m *mockClient) CountDocuments(ctx context.Context, db, coll string, filter any,
	_ ...*options.CountOptions) (int64, error) {
	if m.countFn != nil {
		return m.countFn(ctx, db, coll, filter)
	}
	return 0, nil
}

func (m *mockClient) EnsureIndexes(ctx context.Context, db, coll string, models []mongo.IndexModel,
	_ ...*options.CreateIndexesOptions) ([]string, error) {
	if m.ensureIndexesFn != nil {
		return m.ensureIndexesFn(ctx, db, coll, models)
	}
	return nil, nil
}

func (m *mockClient) Transaction(ctx context.Context, fn TxFunc, opts ...TxOption) error {
	if m.transactionFn != nil {
		return m.transactionFn(ctx, fn, opts...)
	}
	return nil
}

func (m *mockClient) Close(ctx context.Context) error {
	if m.closeFn != nil {
		return m.closeFn(ctx)
	}
	return nil
}

// TestMockClientInterfaceCompliance verifies mockClient implements Client.
func TestMockClientInterfaceCompliance(t *testing.T) {
	var _ Client = (*mockClient)(nil)
}

// TestDefaultClientInterfaceCompliance verifies defaultClient implements Client.
func TestDefaultClientInterfaceCompliance(t *testing.T) {
	var _ Client = (*defaultClient)(nil)
}

// TestNewDefaultClient verifies the constructor wires up startSession.
func TestNewDefaultClient(t *testing.T) {
	mc := &mongo.Client{}
	dc := newDefaultClient(mc)
	require.NotNil(t, dc)
	assert.Same(t, mc, dc.client)
	assert.NotNil(t, dc.startSession)
}

func TestMockClientDispatch(t *testing.T) {
	ctx := context.Background()

	t.Run("InsertOne dispatches and propagates error", func(t *testing.T) {
		want := errors.New("insert err")
		mc := &mockClient{
			insertOneFn: func(_ context.Context, _, _ string, _ any) (*mongo.InsertOneResult, error) {
				return nil, want
			},
		}
		_, err := mc.InsertOne(ctx, "db", "c", bson.M{"k": "v"})
		assert.ErrorIs(t, err, want)
	})

	t.Run("InsertMany default returns empty result", func(t *testing.T) {
		res, err := (&mockClient{}).InsertMany(ctx, "db", "c", []any{1, 2, 3})
		require.NoError(t, err)
		assert.NotNil(t, res)
	})

	t.Run("UpdateOne dispatches", func(t *testing.T) {
		called := false
		mc := &mockClient{
			updateOneFn: func(_ context.Context, _, _ string, _, _ any) (*mongo.UpdateResult, error) {
				called = true
				return &mongo.UpdateResult{ModifiedCount: 1}, nil
			},
		}
		res, err := mc.UpdateOne(ctx, "db", "c", bson.M{}, bson.M{"$set": bson.M{"k": "v"}})
		require.NoError(t, err)
		assert.True(t, called)
		assert.Equal(t, int64(1), res.ModifiedCount)
	})

	t.Run("UpdateMany default returns empty result", func(t *testing.T) {
		res, err := (&mockClient{}).UpdateMany(ctx, "db", "c", bson.M{}, bson.M{})
		require.NoError(t, err)
		assert.NotNil(t, res)
	})

	t.Run("DeleteOne / DeleteMany default to empty results", func(t *testing.T) {
		res1, err := (&mockClient{}).DeleteOne(ctx, "db", "c", bson.M{})
		require.NoError(t, err)
		assert.NotNil(t, res1)

		res2, err := (&mockClient{}).DeleteMany(ctx, "db", "c", bson.M{})
		require.NoError(t, err)
		assert.NotNil(t, res2)
	})

	t.Run("FindOne / FindOneAndUpdate / Find / Aggregate dispatch", func(t *testing.T) {
		var calls int
		mc := &mockClient{
			findOneFn: func(_ context.Context, _, _ string, _ any) *mongo.SingleResult {
				calls++
				return nil
			},
			findOneAndUpdateFn: func(_ context.Context, _, _ string, _, _ any) *mongo.SingleResult {
				calls++
				return nil
			},
			findFn: func(_ context.Context, _, _ string, _ any) (*mongo.Cursor, error) {
				calls++
				return nil, nil
			},
			aggregateFn: func(_ context.Context, _, _ string, _ any) (*mongo.Cursor, error) {
				calls++
				return nil, nil
			},
		}
		mc.FindOne(ctx, "db", "c", bson.M{})
		mc.FindOneAndUpdate(ctx, "db", "c", bson.M{}, bson.M{})
		_, _ = mc.Find(ctx, "db", "c", bson.M{})
		_, _ = mc.Aggregate(ctx, "db", "c", bson.A{})
		assert.Equal(t, 4, calls)
	})

	t.Run("CountDocuments dispatches", func(t *testing.T) {
		mc := &mockClient{
			countFn: func(_ context.Context, _, _ string, _ any) (int64, error) {
				return 42, nil
			},
		}
		n, err := mc.CountDocuments(ctx, "db", "c", bson.M{})
		require.NoError(t, err)
		assert.Equal(t, int64(42), n)
	})

	t.Run("EnsureIndexes dispatches", func(t *testing.T) {
		mc := &mockClient{
			ensureIndexesFn: func(_ context.Context, _, _ string, models []mongo.IndexModel) ([]string, error) {
				names := make([]string, len(models))
				for i := range models {
					names[i] = "idx"
				}
				return names, nil
			},
		}
		got, err := mc.EnsureIndexes(ctx, "db", "c",
			[]mongo.IndexModel{{Keys: bson.D{{Key: "k", Value: 1}}}, {Keys: bson.D{{Key: "k2", Value: 1}}}})
		require.NoError(t, err)
		assert.Equal(t, []string{"idx", "idx"}, got)
	})

	t.Run("Transaction dispatches and forwards options", func(t *testing.T) {
		var receivedOpts int
		mc := &mockClient{
			transactionFn: func(_ context.Context, fn TxFunc, opts ...TxOption) error {
				receivedOpts = len(opts)
				return fn(nil)
			},
		}
		called := false
		err := mc.Transaction(ctx, func(_ mongo.SessionContext) error {
			called = true
			return nil
		}, WithTransactionOptions(options.Transaction()))
		require.NoError(t, err)
		assert.True(t, called)
		assert.Equal(t, 1, receivedOpts)
	})

	t.Run("Close dispatches", func(t *testing.T) {
		var called bool
		mc := &mockClient{
			closeFn: func(_ context.Context) error {
				called = true
				return nil
			},
		}
		require.NoError(t, mc.Close(ctx))
		assert.True(t, called)
	})
}

// fakeSession is a stub session used to drive defaultClient.Transaction
// without a real MongoDB connection.
type fakeSession struct {
	endCalled       bool
	withTxErr       error
	withTxOptsCount int
	withTxFnRunErr  error
	withTxRanFn     bool
}

func (f *fakeSession) EndSession(_ context.Context) {
	f.endCalled = true
}

func (f *fakeSession) WithTransaction(ctx context.Context, fn func(sc mongo.SessionContext) (any, error),
	opts ...*options.TransactionOptions) (any, error) {
	f.withTxOptsCount = len(opts)
	if f.withTxErr != nil {
		return nil, f.withTxErr
	}
	// Drive the user fn with a nil SessionContext - we only verify dispatch.
	f.withTxRanFn = true
	if _, err := fn(nil); err != nil {
		f.withTxFnRunErr = err
		return nil, err
	}
	return nil, nil
}

func TestDefaultClient_Transaction(t *testing.T) {
	ctx := context.Background()

	t.Run("start session error is wrapped", func(t *testing.T) {
		dc := &defaultClient{
			startSession: func(_ ...*options.SessionOptions) (session, error) {
				return nil, errors.New("boom")
			},
		}
		err := dc.Transaction(ctx, func(_ mongo.SessionContext) error { return nil })
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mongodb: start session")
		assert.Contains(t, err.Error(), "boom")
	})

	t.Run("success path runs fn and ends session", func(t *testing.T) {
		fs := &fakeSession{}
		dc := &defaultClient{
			startSession: func(_ ...*options.SessionOptions) (session, error) { return fs, nil },
		}
		ran := false
		err := dc.Transaction(ctx, func(_ mongo.SessionContext) error {
			ran = true
			return nil
		})
		require.NoError(t, err)
		assert.True(t, ran)
		assert.True(t, fs.endCalled)
		assert.Equal(t, 0, fs.withTxOptsCount, "no transaction options forwarded by default")
	})

	t.Run("fn error propagates", func(t *testing.T) {
		fs := &fakeSession{}
		dc := &defaultClient{
			startSession: func(_ ...*options.SessionOptions) (session, error) { return fs, nil },
		}
		want := errors.New("fn err")
		err := dc.Transaction(ctx, func(_ mongo.SessionContext) error { return want })
		require.ErrorIs(t, err, want)
		assert.True(t, fs.endCalled)
		assert.True(t, fs.withTxRanFn)
		assert.ErrorIs(t, fs.withTxFnRunErr, want)
	})

	t.Run("WithTransaction error propagates", func(t *testing.T) {
		want := errors.New("tx err")
		fs := &fakeSession{withTxErr: want}
		dc := &defaultClient{
			startSession: func(_ ...*options.SessionOptions) (session, error) { return fs, nil },
		}
		err := dc.Transaction(ctx, func(_ mongo.SessionContext) error { return nil })
		require.ErrorIs(t, err, want)
		assert.True(t, fs.endCalled)
	})

	t.Run("transaction and session options are forwarded", func(t *testing.T) {
		fs := &fakeSession{}
		var sessOpts []*options.SessionOptions
		dc := &defaultClient{
			startSession: func(opts ...*options.SessionOptions) (session, error) {
				sessOpts = opts
				return fs, nil
			},
		}
		err := dc.Transaction(ctx, func(_ mongo.SessionContext) error { return nil },
			WithTransactionOptions(options.Transaction()),
			WithSessionOptions(options.Session()),
		)
		require.NoError(t, err)
		assert.Equal(t, 1, fs.withTxOptsCount)
		assert.Len(t, sessOpts, 1)
	})
}

func TestEnsureIndexesEmpty(t *testing.T) {
	dc := &defaultClient{}
	names, err := dc.EnsureIndexes(context.Background(), "db", "c", nil)
	require.NoError(t, err)
	assert.Nil(t, names)
}

func TestTxOptionConstructors(t *testing.T) {
	t.Run("WithTransactionOptions sets field", func(t *testing.T) {
		o := &TxOptions{}
		got := options.Transaction()
		WithTransactionOptions(got)(o)
		assert.Same(t, got, o.Transaction)
	})

	t.Run("WithSessionOptions sets field", func(t *testing.T) {
		o := &TxOptions{}
		got := options.Session()
		WithSessionOptions(got)(o)
		assert.Same(t, got, o.Session)
	})
}

func TestDefaultClient_Coll(t *testing.T) {
	// We can't construct a usable *mongo.Client without a real driver session
	// (the type's internals are not exported), but we *can* exercise the
	// helper's nil-check path for coverage.
	defer func() {
		if r := recover(); r != nil {
			// expected: dereferencing nil deployment will panic
			return
		}
		t.Fatal("expected panic on nil mongo.Client")
	}()
	dc := &defaultClient{}
	_ = dc.coll("db", "c")
}
