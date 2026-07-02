# 技术设计：Artifact TTL / Retention 基础能力

## 1. 文档定位
本文是需求包 J-1 的执行技术方案，配套需求范围见 `req-scope.md`。

本文只描述技术实现边界、API 形态、backend 方案、失败语义和测试策略。需求范围变更优先修改 `req-scope.md`，技术实现再按需求范围同步调整。

J-1 在 `ArtifactService` 层建立 TTL / retention 的完整闭环：
- 配置。
- 保存。
- 过期判定。
- metadata 查询。
- dry-run 扫描。
- 后台 cleanup。
- cleanup 结果可见。

J-1 可以在 ArtifactService 自身范围内删除已过期且未 pinned 的 artifact version。引用关系驱动的安全清理、orphan 判断和治理级生命周期策略放到 J-2。

## 2. 设计约束
### 2.1 兼容性
遵循 `..dev/.checklist/code.md`：
- 不改变现有 `artifact.Service` 接口签名。
- 不要求现有 `SaveArtifact` / `LoadArtifact` / `DeleteArtifact` 调用方修改代码。
- 非必要不扩大导出面；新增导出项必须服务于 J-1 的完整闭环。
- 不变更 Go 版本要求。

### 2.2 对齐 session TTL 习惯
Session backend 的既有做法：
- 对业务暴露相对 TTL duration，例如 `WithSessionTTL(30*time.Minute)`。
- backend 内部把 TTL 转换为绝对 `expires_at`。
- `expires_at` 用于索引、过滤和 cleanup，不作为业务配置主入口。
- TTL 配置后，除 Redis 这类原生 TTL backend 外，多数 backend 会启动后台 cleanup。

Artifact J-1 沿用该风格：
- 业务配置使用相对 TTL duration。
- 内部 lifecycle 标准字段包含 `expires_at`。
- lifecycle 查询结果可以返回 `expires_at`。
- 未配置 TTL 时不写入过期语义。
- 后台 cleanup 由 service-level 默认 TTL 或显式 cleanup 配置触发，不与 service-level 默认 TTL 强绑定。

## 3. 当前实现基线
### 3.1 公开 API
当前 artifact 公开结构：
- `artifact.Artifact`：
    - `Data`。
    - `MimeType`。
    - `URL`。
    - `Name`。
- `artifact.Service`：
    - `SaveArtifact`。
    - `LoadArtifact`。
    - `ListArtifactKeys`。
    - `DeleteArtifact`。
    - `ListVersions`。

当前 artifact backend：
- `artifact/inmemory`。
- `artifact/cos`。
- `artifact/s3`。

### 3.2 关键现状
- artifact version 由框架维护。
- S3 / COS 中每个 artifact version 是独立 object key。
- `DeleteArtifact` 当前按 artifact name 删除所有 versions，不提供公开 version-level delete。
- `artifact/s3` 当前 object key 没有统一 `artifact/` 根前缀。
- `artifact/cos` 当前新路径有 `artifact/` 根前缀，但仍兼容 legacy 无根路径。
- `storage/s3.Client` 当前只暴露 put / get / list / delete body 级操作，没有 metadata-only read。
- `artifact/cos` 当前 `PutObject` 可传完整 `ObjectPutOptions`，但 client interface 无 metadata-only read。

## 4. API 方案
### 4.1 保持 `artifact.Service` 不变
`artifact.Service` 继续作为最小 CRUD 接口，不新增方法，不改签名。

原因：
- 保护业务已有实现和调用方。
- 第三方自定义 artifact service 不会因升级而编译失败。
- J-1 新能力通过额外可选接口暴露。

### 4.2 Service-level 配置
各 backend 提供与 session 风格一致的配置项。

建议配置项：
- `WithTTL(ttl time.Duration)` 或 backend 包内等价 option。
- `WithCleanupInterval(interval time.Duration)`。
- `WithCleanupEnabled(enabled bool)`，可作为可选语法糖；如果不提供该 option，则以 `WithCleanupInterval` 表达显式启用。

配置语义：
- `ttl <= 0`：
    - 不设置 service-level 默认 TTL。
    - 仍允许单次保存通过 option 设置 TTL。
- `ttl > 0`：
    - 新保存 artifact 默认写入 `expires_at = now + ttl`。
- `cleanupInterval > 0`：
    - 按指定间隔启动后台 cleanup。
- `cleanupInterval <= 0 && ttl > 0`：
    - 使用 backend 默认 cleanup interval。
    - 默认值建议沿用 session 常用值 `5 * time.Minute`。
    - 对象存储 backend 如需调整，需要在文档中说明。
- `cleanupInterval <= 0 && ttl <= 0`：
    - 默认不启动后台 cleanup。
    - 如果业务只使用 per-save TTL，需要显式配置 `WithCleanupInterval` 或等价 cleanup enable option。
- `WithCleanupEnabled(true)` 且未配置 cleanup interval：
    - 使用 backend 默认 cleanup interval。

兼容性说明：
- `artifact/inmemory.NewService` 当前无 options，可改为 `NewService(opts ...Option)`。
- 普通调用 `NewService()` 保持源码兼容。
- 直接把 `NewService` 作为函数值使用的极少数场景可能受影响，实现时需评估。

### 4.3 单次保存覆盖
新增可选接口支持单次保存覆盖 service-level 默认 TTL / retention。

建议形态如下。类型位于 `artifact` 包，代码块中省略包名前缀：
```go
type SaveOption func(*SaveOptions)

type SaveOptions struct {
	TTL             *time.Duration
	RetentionPolicy string
	Pinned          bool
}

type SaveOptionsService interface {
	SaveArtifactWithOptions(
		ctx context.Context,
		sessionInfo SessionInfo,
		filename string,
		artifact *Artifact,
		opts ...SaveOption,
	) (int, error)
}
```

保存语义：
- `TTL == nil`：
    - 使用 service-level 默认 TTL。
- `TTL <= 0`：
    - 该次保存不设置过期时间。
- `Pinned == true`：
    - 该 version 不参与默认 TTL cleanup。
- 具体命名由实现阶段按包风格微调，但必须满足“默认 TTL + 单次覆盖”的需求语义。

不建议把 lifecycle 字段直接塞进 `artifact.Artifact` 作为主要入口。`Artifact` 当前是内容载体，直接扩展会让所有保存路径都暴露生命周期配置；使用可选接口和 save option 更符合导出面控制。

### 4.4 生命周期信息查询
新增可选接口读取 version-level lifecycle 信息。

建议形态如下。类型位于 `artifact` 包，代码块中省略包名前缀：
```go
type LifecycleMetadata struct {
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ExpiresAt       time.Time
	RetentionPolicy string
	Pinned          bool
}

type VersionInfo struct {
	Filename string
	Version  int
	LifecycleMetadata
}

type MetadataService interface {
	GetArtifactInfo(
		ctx context.Context,
		sessionInfo SessionInfo,
		filename string,
		version int,
	) (*VersionInfo, error)

	ListVersionInfos(
		ctx context.Context,
		sessionInfo SessionInfo,
		filename string,
	) ([]VersionInfo, error)
}
```

查询语义：
- 历史 artifact 无 lifecycle metadata 时返回稳定零值，例如 `ExpiresAt.IsZero()`。
- `LoadArtifact` 仍只负责内容读取，不因 metadata 缺失改变行为。
- `expires_at` 对查询结果可见，但业务配置主入口仍是 TTL duration。
- artifact version 是不可变对象。
- `CreatedAt` 由保存时写入。
- `UpdatedAt` 第一版可等于 `CreatedAt` 或保持零值，但同一 backend 内必须稳定。
- 对象存储建议写入 `updated_at = created_at`，避免查询结果出现不必要的 backend 差异。

### 4.5 Capability 与错误语义
新增能力必须能被业务和测试稳定识别，不能只依赖错误字符串。

建议形态如下。类型位于 `artifact` 包，代码块中省略包名前缀：
```go
var (
	ErrLifecycleUnsupported    = errors.New("artifact lifecycle unsupported")
	ErrCleanupScopeUnsupported = errors.New("artifact cleanup scope unsupported")
)

type LifecycleCapabilities struct {
	SupportsMetadata       bool
	SupportsSessionScan    bool
	SupportsUserScan       bool
	SupportsAppScan        bool
	SupportsSessionCleanup bool
	SupportsUserCleanup    bool
	SupportsAppCleanup     bool
	SupportsBackgroundGC   bool
}

type CapabilityService interface {
	LifecycleCapabilities(ctx context.Context) LifecycleCapabilities
}
```

错误语义：
- backend 完全不支持 J-1 lifecycle 能力时，返回 `ErrLifecycleUnsupported`。
- backend 支持 lifecycle 但不支持某个 scope 时，返回 `ErrCleanupScopeUnsupported`。
- `artifact/s3.WithClient` 注入的自定义 client 如果缺少 metadata head / put 能力：
    - 普通 CRUD 继续可用。
    - lifecycle 能力返回 `ErrLifecycleUnsupported` 或 capability 中标记不支持。

### 4.6 扫描与 cleanup
新增可选接口支持 session-scoped / user-scoped scan 和 cleanup。scan 是观测能力，cleanup 是执行能力，两者分离。

建议形态如下。类型位于 `artifact` 包，代码块中省略包名前缀：
```go
type CleanupScope string

const (
	CleanupScopeSession CleanupScope = "session"
	CleanupScopeUser    CleanupScope = "user"
	CleanupScopeApp     CleanupScope = "app"
)

type CleanupTarget struct {
	AppName   string
	UserID    string
	SessionID string
}

type ScanExpiredArtifactsRequest struct {
	Target        CleanupTarget
	Scope         CleanupScope
	Now           time.Time
	ExpiresBefore time.Time
}

type ArtifactLifecycleCandidate struct {
	Target          CleanupTarget
	Filename        string
	Version         int
	ExpiresAt       time.Time
	RetentionPolicy string
	Pinned          bool
	Expired         bool
	Reason          string
}

type ScanExpiredArtifactsResult struct {
	Items []ArtifactLifecycleCandidate
}

type CleanupRequest struct {
	Target CleanupTarget
	Scope  CleanupScope
	Now    time.Time
}

type CleanupResult struct {
	Deleted      int
	Skipped      int
	Failed       int
	ReasonCounts map[string]int
}

type LifecycleService interface {
	ScanExpiredArtifacts(
		ctx context.Context,
		req ScanExpiredArtifactsRequest,
	) (*ScanExpiredArtifactsResult, error)

	CleanupExpiredArtifacts(
		ctx context.Context,
		req CleanupRequest,
	) (*CleanupResult, error)
}
```

J-1 硬验收范围：
- `CleanupScopeSession`。
- `CleanupScopeUser`。

`CleanupScopeApp` 不作为 J-1 硬验收。backend 可通过 capability 明示支持或返回不支持错误。

scope target 校验：
- `CleanupScopeSession`：
    - 需要 `AppName`、`UserID`、`SessionID`。
- `CleanupScopeUser`：
    - 需要 `AppName`、`UserID`。
    - 不要求 `SessionID`。
- `CleanupScopeApp`：
    - 需要 `AppName`。
    - 不要求 `UserID` 和 `SessionID`。

scan 判断顺序：
1. 无 lifecycle metadata：
    - skip，原因 `missing_metadata`。
2. lifecycle metadata 格式错误：
    - skip，原因 `malformed_metadata`。
3. `pinned == true`：
    - skip，原因 `pinned`。
4. `expires_at` 为空：
    - skip，原因 `no_expiry`。
5. `expires_at > now`：
    - skip，原因 `not_expired`。
6. 已过期且未 pinned：
    - 作为 cleanup candidate。

scan 结果要求：
- 返回逐 artifact version 明细。
- 至少包含 target、filename、version、`expires_at`、retention、pinned、expired 和 reason。
- scan 即 dry-run cleanup：只展示当前条件下会被 cleanup 选中的对象和跳过原因，不删除任何 artifact。

`Now` 与 `ExpiresBefore`：
- `Now` 为空时使用当前时间。
- `ExpiresBefore` 为空时等同于 `Now`，只扫描已过期对象。
- `ExpiresBefore > Now` 时，可用于查看即将过期对象。
- J-1 硬验收只要求 expired scan，不要求 soon-to-expire lookahead。

`CleanupResult` 默认保持聚合统计。如果实现阶段认为逐项 cleanup 明细有必要，可在不破坏基础语义的前提下补充可选 detail。

### 4.7 后台 cleanup
后台 cleanup 使用同一套扫描 / 删除逻辑。

启用语义：
- service-level TTL 启用时自动启动后台 cleanup。
- 没有 service-level TTL 但业务使用 per-save TTL 时，可通过 `WithCleanupInterval` 或等价 cleanup enable option 显式启动后台 cleanup。

执行语义：
- 后台 cleanup 不要求业务方逐个传入 session / user scope。
- backend 需要使用内部全前缀扫描、持久化 scope 索引或等价机制完成自动清理。
- 显式 scan / cleanup API 的 J-1 硬验收是 session scope 与 user scope。
- app/global scope 可作为 capability 暴露，但后台 cleanup 的实现不能因此变成空转。
- 后台 cleanup 结果通过日志可见，至少包含 deleted / skipped / failed 和主要原因计数。
- 后台 cleanup 是 eventual cleanup，不保证 TTL 到点立即删除。

### 4.8 后台任务生命周期
所有启动后台 cleanup 的 backend 都需要定义可关闭的生命周期。

要求：
- service 初始化成功后才能启动 goroutine；初始化失败不得泄漏 goroutine。
- 启动后台 cleanup 的 service 必须实现 `Close()`，或已有 `Close()` 必须停止 cleanup。
- `Close()` 需要停止 ticker，并通过 context、done channel 或 wait group 等方式等待当前 cleanup 退出。
- 后台 cleanup 使用 mutex / atomic guard 防止重入。
- 如果上一轮尚未结束，下一轮应跳过并记录日志。
- 显式 cleanup 和后台 cleanup 复用同一套内部逻辑。
- 显式 cleanup 与后台 cleanup 并发执行时要有互斥策略，避免重复删除同一 version。
- 每轮 cleanup 需要有 context timeout，防止对象存储 list / head / delete 长时间阻塞。

### 4.9 lifecycle 能力失败时机
启用 TTL / lifecycle 能力时，不应静默降级。

要求：
- 如果 backend 初始化时已经能判断 lifecycle 能力不可用，`NewService` 应 fail-fast。
    - 例如显式启用 cleanup 但缺少必要 root prefix / index 配置。
- 如果能力取决于用户注入的 client，应在第一次需要该能力时返回稳定错误。
    - 例如自定义 S3 client 是否支持 metadata put / head。
    - service 也可以在 `NewService` 中通过 interface assertion 提前 fail-fast。
- 配置了 service-level TTL，或调用 `SaveArtifactWithOptions` 设置 TTL / retention / pinned 时，如果 backend 无法写 lifecycle metadata，对应保存调用必须返回错误。
- 不允许悄悄保存成无 TTL artifact。
- 普通 `SaveArtifact` 在未配置 service-level TTL、未使用 lifecycle option 时仍保持旧行为。

## 5. Backend 实现
### 5.1 In-memory
当前结构为 `map[path][]*artifact.Artifact`。

建议改为内部 entry：
```go
type versionEntry struct {
	artifact *artifact.Artifact
	meta     artifact.LifecycleMetadata
}
```

实现要点：
- `SaveArtifact` 写入时，如果 service TTL 启用，则计算 `ExpiresAt = now + ttl`。
- `SaveArtifactWithOptions` 根据单次 options 覆盖默认 TTL / retention / pinned。
- `LoadArtifact` 只返回 artifact 内容。
- `GetArtifactInfo` / `ListVersionInfos` 读取 entry meta。
- cleanup 遍历 map，按 session / user prefix 过滤并删除具体 version entry。
- 为保持版本号稳定，删除 version entry 后不能简单重排已有版本号。
- 建议保留 tombstone 或维护 version -> entry 映射。

跨 backend 统一版本语义：
- cleanup 删除某个 version 后，该 version 不再出现在 `ListVersions` 中。
- `LoadArtifact` 指定已删除 version 时返回 not found / nil。
- `LoadArtifact` 的 version 为 nil 时，返回最高的未删除 version。
- 下一次 `SaveArtifact` 使用历史最大 version + 1，不复用已删除 version。
- `ListVersionInfos` 跳过已删除 tombstone。
- 如实现需要暴露 tombstone，必须通过额外内部状态，不改变公开 `ListVersions` 语义。

### 5.2 Version allocator
J-1 引入 version-level cleanup 后，不能再只依赖“list 现存 version object keys 后 max + 1”分配版本号。否则删除最高 version 后，下一次保存会复用 version，导致历史 `artifact://name@version` 可能读到新内容。

各 backend 需要维护“历史最大 version”：
- in-memory：
    - 在 entry map 中保存每个 artifact path 的 `nextVersion` 或 `maxVersionSeen`。
    - cleanup 删除 tombstone 不回退该值。
- S3-compatible：
    - 为每个 artifact path 写入 sidecar allocator object。
- COS：
    - 采用与 S3-compatible 相同的 sidecar allocator object。

建议 sidecar key：
- `{artifact_object_prefix}.meta/version_allocator`。
- `artifact_object_prefix` 是不含 version 的 artifact object prefix。
- session artifact 示例：
    - `{app}/{user}/{session}/{filename}/.meta/version_allocator`。
- user artifact 示例：
    - `{app}/{user}/user/{filename}/.meta/version_allocator`。
- COS 当前新路径带 `artifact/` 根前缀时，sidecar 也位于对应新路径下。
- legacy 路径只读兼容，不为旧路径补建 allocator，直到新保存发生时再写入新路径 allocator。

allocator 内容建议：
```json
{
  "next_version": 3,
  "updated_at": "2026-07-02T00:00:00Z"
}
```

保存流程：
1. 读取 allocator。
2. allocator 存在时使用 `next_version`。
3. allocator 不存在时，为兼容历史数据，list 现存 version object keys，取 `max(existing_versions)+1` 作为 `next_version`。
4. 先更新 allocator 为 `next_version + 1`，完成 version 预留。
5. 再写入 artifact version object。

失败语义：
- allocator 写入失败时：
    - `SaveArtifact` 返回错误。
    - 不写 artifact object。
- artifact object 写入失败时：
    - 已预留的 version 可以形成 gap。
    - 不得回退 allocator。
    - 不得复用 version。
- version gap 不出现在 `ListVersions` 中，因为没有对应 artifact version object。
- “先预留，后写对象”的顺序优先保证不复用 version；代价是失败时可能跳号，这是可接受的。

并发语义：
- 当前 S3 backend 已有并发保存同一 artifact 可能复用 version 的风险，J-1 不扩大该风险。
- allocator 第一版可保持“best effort + 文档说明”，不强制实现分布式 CAS。
- 如果底层 client 支持 conditional put / ETag CAS，可作为 backend 优化，但不作为 J-1 硬验收。

cleanup 语义：
- cleanup 删除 version object 时不删除或回退 allocator。
- TTL cleanup 不等同于业务主动 `DeleteArtifact`，不得删除 allocator。
- `DeleteArtifact` 删除所有 versions 时可以删除 allocator。
- `DeleteArtifact` 后再次保存同名 artifact 是否从 0 开始，维持现有 `DeleteArtifact` 语义。

### 5.3 S3-compatible (`artifact/s3`)
目标覆盖 AWS S3、MinIO、R2 等共同能力集。

约束：
- 不依赖 object tagging。
- 不依赖 bucket lifecycle。
- 不依赖 S3 native versioning。
- 使用框架 version object key 作为 cleanup 粒度。

#### 底层 client 扩展
不直接修改 `storage/s3.Client` 必需方法集合，以免破坏外部自定义 client。

新增可选能力接口，例如 metadata put / head：
```go
type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
	Metadata     map[string]string
}

type MetadataClient interface {
	PutObjectWithMetadata(
		ctx context.Context,
		key string,
		data []byte,
		contentType string,
		metadata map[string]string,
	) error

	HeadObject(
		ctx context.Context,
		key string,
	) (*ObjectInfo, error)
}
```

client 语义：
- 默认 `storage/s3` client 实现该接口。
- 用户通过 `artifact/s3.WithClient` 传入的自定义 client 若不支持 metadata 能力：
    - 普通 artifact CRUD 继续可用。
    - TTL lifecycle 能力返回明确不支持错误。

#### Object metadata schema
metadata 约定：
- 使用 ASCII 小写 key，避免 R2 / HTTP header 兼容问题。
- 读取 metadata 时需要处理 provider 对 metadata key 的大小写归一化。

建议字段：
- `trpc-agent-artifact-created-at`。
- `trpc-agent-artifact-updated-at`。
- `trpc-agent-artifact-expires-at`。
- `trpc-agent-artifact-retention-policy`。
- `trpc-agent-artifact-pinned`。

字段语义：
- 时间使用 RFC3339 UTC。
- `pinned` 使用 `true` / `false` 字符串。
- `retention_policy` 只作为字符串表达，不在 J-1 中解释复杂业务语义。
- `expires_at` 解析失败时不删除。
- `expires_at` 解析失败时，scan reason 记为 `malformed_metadata`。
- cleanup 对 malformed metadata 计入 skipped 或 failed 需保持一致，并在 `ReasonCounts` 中可见。
- missing metadata 与 malformed metadata 必须区分：
    - `missing_metadata`。
    - `malformed_metadata`。

#### 显式 scan / cleanup
扫描与删除：
- session scope 使用 `BuildSessionPrefix`。
- user scope 使用 `BuildUserNamespacePrefix`。
- 先 list object keys。
- 再对候选 key 执行 `HeadObject` 读取 metadata。
- cleanup 非 dry-run 使用现有 delete object 能力删除具体 version object key。

#### 后台 cleanup
当前 S3 object key 格式没有统一 `artifact/` 根前缀，公共 root prefix 可能为空。J-1 不应默认扫描整个 bucket。

后台 cleanup 需要显式发现边界。第一版推荐新增 backend option：
- `WithCleanupRootPrefixes(prefixes ...string)`。
- 或等价配置。

root prefix 语义：
- root prefix 必须覆盖当前 artifact key 规则下的 app / user / session namespace。
- 业务可传 app 级前缀，例如 `{app}/`。
- 多 app 则传多个 prefix。
- 未配置 root prefix 且未启用 lifecycle index 时：
    - S3 backend 不启动 background GC。
    - capability 标记 `SupportsBackgroundGC=false`。
    - 如果用户显式启用 cleanup，应 fail-fast 返回配置错误。

每轮 cleanup 约束：
- 使用分页 list，不能一次性加载全部 keys。
- 需要有 budget：
    - `maxObjects`。
    - `maxDeletes`。
    - `maxDuration`。
- 默认值由 backend options 决定，第一版可使用保守默认值并在文档中说明。
- list 到 key 后优先通过 `HeadObject` 读取 metadata。
- 不得读取完整 body 作为常态扫描路径。
- 支持批量删除时优先使用批量 delete。
- 不支持批量删除时逐个 delete，并在结果中统计失败。
- 对 head / delete 做有限并发和限流，避免后台 cleanup 对 bucket 产生突刺。
- app/global scan 不是 J-1 显式 API 硬验收。
- 后台 cleanup 可以使用内部 root-prefix scan 完成自动清理，并通过 budget 控制成本。
- 如果 backend 无法提供可接受的 root-prefix scan，应在 capability 中标记不支持 background GC，而不是启动空转任务。

可选优化：
- 高规模场景可引入 lifecycle index。
- 保存 artifact 时写入轻量 index object。
- 后台 cleanup 扫 index，而不是扫 object namespace。
- lifecycle index 会引入额外一致性状态，不作为 J-1 默认方案。
- J-1 默认采用显式 root prefix + budgeted list / head / delete。

### 5.4 COS (`artifact/cos`)
COS 与 S3 的语义保持一致。

实现要点：
- 上传时通过 `cos.ObjectPutOptions` 写入 `x-cos-meta-*` 自定义 metadata。
- 新增 COS client metadata-only read 能力。
- 若 SDK Head API 可用，优先使用 Head。
- 不能通过读取完整 body 作为常态扫描路径。
- 显式 scan 需要同时考虑当前 `artifact/` 根前缀和 legacy key 前缀，沿用现有 `build*Candidates` 兼容模式。
- cleanup 删除具体 version object key。

后台 cleanup 策略：
- COS 新路径有 `artifact/` 根前缀。
- COS 仍兼容 legacy 无根路径。
- 后台 cleanup 默认只扫描新路径 `artifact/` 根前缀。
- 如果业务需要治理 legacy 无根路径，需要显式配置 legacy root prefixes，例如 `{app}/`。
- 不能为了兼容 legacy 默认扫描整个 bucket。
- legacy 对象缺少 lifecycle metadata 时按 `missing_metadata` 跳过，不做历史补写。
- 分页、Head metadata、预算、限流、批量删除和结果统计规则与 S3-compatible backend 保持一致。
- COS 原生 bucket lifecycle 只能作为粗粒度运维兜底，不作为 J-1 TTL 语义的主实现。

### 5.5 旧 artifact 兼容
旧 artifact 没有 lifecycle metadata：
- 继续可以 `LoadArtifact`。
- 不参与默认 TTL cleanup。
- 查询 lifecycle info 时返回零值 metadata，并标注原因 `missing_metadata` 或等价语义。

J-1 不做历史 artifact 补写 metadata；如需批量补写，归后续迁移或治理工具。

## 6. Cleanup 与引用关系边界
J-1 cleanup 不扫描 session / tool / workspace / eval 引用关系。

业务启用 TTL 后，过期 artifact 即可能被 ArtifactService cleanup 删除，即使历史引用仍存在。该行为必须在文档中明确说明。

J-2 负责：
- owner / reference 追踪。
- orphan 判断。
- session 删除联动。
- expired + orphan 的治理级安全清理。
- cleanup 失败重试、告警和人工修复闭环。

## 7. 测试计划
### 7.1 通用行为
- 未配置 TTL：现有 save / load / list / delete / list versions 测试不变。
- 配置 service-level TTL：新保存 artifact version 拥有 `expires_at`。
- 单次保存覆盖 TTL：覆盖 service-level 默认 TTL。
- 单次保存设置无过期：不参与 cleanup。
- 未配置 service-level TTL 但单次保存设置 TTL：
    - artifact 拥有 `expires_at`。
    - 显式 scan / cleanup 可处理。
    - 显式启用后台 cleanup 后后台任务也能处理。
- pinned version：即使过期也不被 cleanup 删除。
- 历史无 metadata artifact：可读取，不被 cleanup 删除。
- capability 与 unsupported error：不支持 lifecycle、metadata 或某 scope 时返回稳定 capability / error。

### 7.2 Scan 与 Cleanup
- scan 能返回逐 artifact version 明细，包含 target、filename、version、expires_at、retention、pinned、expired、reason。
- scan 能识别 expired candidates，但不删除。
- 非 dry-run cleanup 删除 expired + unpinned version。
- cleanup 后该 version `LoadArtifact` 返回 not found。
- cleanup 结果至少区分 deleted / skipped / failed。
- 后台 cleanup 在 service-level TTL 启用后自动运行。
- per-save TTL-only 场景显式启用 cleanup 后，后台 cleanup 能自动运行。
- 后台 cleanup 防重入。
- `Close` 能停止 ticker 并等待当前 cleanup 退出。

### 7.3 Scope
- session-scoped scan / cleanup。
- user-scoped scan / cleanup。
- app/global scope 返回 capability 或不支持错误，除非 backend 实现明确支持。
- 不同 scope 按 `CleanupTarget` 校验必填字段。

### 7.4 Backend
`artifact/inmemory`：
- 单元测试覆盖完整语义。
- 稀疏 version 测试：删除 version 后 `ListVersions`、`LoadArtifact(nil)`、下一次 `SaveArtifact` 的版本号语义一致。

`artifact/s3`：
- 使用 mock metadata-capable client 覆盖 save metadata、head metadata、scan、delete。
- 覆盖后台 cleanup 的 root-prefix scan、分页、预算、限流和 metadata parse error。
- 覆盖 allocator sidecar：删除最高 version 后继续保存不复用 version。
- 覆盖 allocator 预留成功但 artifact 写入失败时产生 version gap 且后续不复用。
- 覆盖未配置 cleanup root prefix 时 background GC capability / fail-fast 行为。

`storage/s3`：
- client 测试覆盖 metadata put / head。

`artifact/cos`：
- 使用 stub client 覆盖 metadata 写入、head、legacy/current prefix scan、delete。
- 覆盖 legacy prefix 缺 metadata 时跳过并返回 `missing_metadata` reason。
- 覆盖 allocator sidecar 与 explicit legacy root prefix。

## 8. 文档与示例
需要更新：
- `docs/mkdocs/en/artifact.md`。
- `docs/mkdocs/zh/artifact.md`。

文档必须说明：
- TTL 使用相对 duration 配置。
- 未配置 service-level 默认 TTL 且单次保存未设置 TTL 时，artifact 不会因 J-1 过期，行为与旧版一致。
- 启用 service-level TTL 或单次保存 TTL 后，artifact 到期只代表进入 cleanup candidate，不代表立即删除。
- 启用后台 cleanup 后，ArtifactService 会删除过期且未 pinned 的 artifact。
- 对象存储后台 cleanup 需要明确 root prefix 边界；未配置时不会默认扫描整个 bucket。
- cleanup 不做引用安全校验；历史 `artifact://...` 引用可能失效。
- 需要引用安全治理时使用 J-2 能力。

示例建议：
- `artifact/inmemory` TTL 示例。
- S3-compatible TTL 配置示例。
- dry-run cleanup 示例。
