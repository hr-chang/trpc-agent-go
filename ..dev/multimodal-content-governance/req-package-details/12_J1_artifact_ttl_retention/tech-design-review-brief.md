# 技术方案简版：Artifact TTL / Retention 基础能力

## 1. 背景
当前 `artifact.Service` 已经提供 artifact 的保存、读取、列举、删除和版本列举能力，但缺少生命周期管理能力。

随着 session 多模态外存、tool/workspace/codeexecutor 输出等场景逐步使用 artifact，artifact 存储会从“辅助能力”变成框架内的大对象承载层。若 artifact 缺少 TTL / retention 能力，业务会面临存储持续膨胀、临时产物无法治理、清理结果不可见等问题。

J-1 的目标是在 ArtifactService 层提供最小但完整的 TTL / retention 闭环，而不是把 TTL 做成 session externalization 的私有逻辑。

## 2. 目标
J-1 提供 artifact 层基础生命周期能力：
- 支持 service-level 默认 TTL。
- 支持单次保存覆盖 TTL / retention / pinned。
- 为每个 artifact version 记录 lifecycle metadata。
- 支持查询 lifecycle metadata。
- 支持显式 scan / dry-run。
- 支持 cleanup 删除已过期且未 pinned 的 artifact version。
- 支持后台定时 cleanup。
- 覆盖 `artifact/inmemory`、`artifact/s3`、`artifact/cos`。

J-1 不解决引用安全问题。cleanup 不判断 artifact 是否仍被 session、tool、workspace、debug/eval 等引用；引用关系追踪、orphan 判断和治理级安全删除放到 J-2。

## 3. 核心设计
### 3.1 保持 `artifact.Service` 兼容
现有 `artifact.Service` 不改签名，继续作为最小 CRUD 接口。

新增 TTL / retention 能力通过可选接口暴露，避免破坏已有调用方和第三方自定义 backend。

### 3.2 生命周期粒度
TTL / retention 绑定到 artifact version，而不是 artifact name。

原因：
- 当前 `SaveArtifact` 每次保存都会返回 version。
- session `ContentRef` 固定引用 `artifact://<name>@<version>`。
- 同名不同版本可能有不同来源、大小、保留周期和引用关系。

### 3.3 Metadata 字段
建议最小 lifecycle metadata：
- `created_at`。
- `updated_at`。
- `expires_at`。
- `retention_policy`。
- `pinned`。

其中：
- `expires_at` 是内部标准过期字段。
- 业务配置入口以相对 TTL duration 为主。
- `pinned` 优先级高于 `expires_at`，即使 artifact 已过期也不应被默认 cleanup 删除。
- `retention_policy` 在 J-1 中只作为可读字符串，不解释复杂业务语义。

### 3.4 Scan 与 Cleanup
J-1 将 scan 和 cleanup 分离：
- scan 是 dry-run cleanup，返回逐 artifact version 明细，不删除数据。
- cleanup 是执行能力，删除已过期且未 pinned 的 artifact version，并返回聚合统计。

scan 结果至少包含：
- app / user / session 范围信息。
- artifact filename。
- version。
- `expires_at`。
- retention / pinned 信息。
- expired 状态。
- 可解释 reason。

cleanup 结果至少包含：
- deleted 数量。
- skipped 数量。
- failed 数量。
- reason counts。

### 3.5 Cleanup 范围
J-1 硬验收：
- session-scoped scan / cleanup。
- user-scoped scan / cleanup。

app/global scan 不作为 J-1 硬验收。backend 可以通过 capability 明示支持或返回 unsupported error。

### 3.6 后台 Cleanup
后台 cleanup 是 eventual cleanup，不保证 TTL 到点立即删除。

启用规则：
- 配置 service-level TTL 时，默认启动后台 cleanup。
- 只使用 per-save TTL 时，需要显式配置 cleanup interval 或等价 cleanup enable option。

后台任务需要：
- 可关闭。
- 防重入。
- 有 context timeout。
- 与显式 cleanup 复用同一套内部逻辑。
- 输出 deleted / skipped / failed 和主要原因计数。

## 4. 对象存储设计
### 4.1 不依赖原生 bucket lifecycle
S3、COS、MinIO、R2 等对象存储通常不具备与 MongoDB TTL index 等价的 per-record TTL 语义。

因此 J-1 不依赖：
- bucket lifecycle。
- object tagging。
- S3 native versioning。
- COS lifecycle rule。

J-1 使用框架层 metadata + scan + cleanup 呈现统一 TTL 行为。

### 4.2 Metadata 读写
对象存储 backend 在保存 artifact version 时写入 object metadata，例如：
- `trpc-agent-artifact-created-at`。
- `trpc-agent-artifact-updated-at`。
- `trpc-agent-artifact-expires-at`。
- `trpc-agent-artifact-retention-policy`。
- `trpc-agent-artifact-pinned`。

扫描时通过 list object keys + head object metadata 判断是否可清理，不能读取完整 body 作为常态扫描路径。

### 4.3 Version 不复用
TTL cleanup 删除某个 version 后，后续保存不得复用该 version。否则历史 `artifact://name@version` 可能读到新内容。

对象存储 backend 需要维护 per artifact path 的 sidecar version allocator：
- 保存前读取 allocator。
- 先预留下一个 version。
- 再写 artifact version object。
- artifact 写失败时可以产生 version gap，但不得回退 allocator 或复用 version。
- TTL cleanup 不删除或回退 allocator。

### 4.4 后台扫描边界
对象存储后台 cleanup 不能默认扫描整个 bucket。

S3 当前 object key 没有统一 `artifact/` 根前缀，因此需要显式配置 cleanup root prefixes，例如 app 级 `{app}/` 前缀。未配置 root prefix 时，S3 backend 不启动 background GC；如果用户显式启用 cleanup，应 fail-fast。

COS 新路径有 `artifact/` 根前缀，后台 cleanup 默认可以扫描新路径。legacy 无根路径需要业务显式配置 legacy root prefix，不能为了兼容 legacy 默认扫描整个 bucket。

## 5. Backend 覆盖
### 5.1 In-memory
可直接在内部 entry 中保存 artifact 内容和 lifecycle metadata。

需要注意：
- 删除 version 后不能重排版本号。
- `LoadArtifact(nil)` 返回最高未删除 version。
- 下一次保存使用历史最大 version + 1。

### 5.2 S3-compatible
覆盖 AWS S3、MinIO、R2 等 S3-compatible storage 的共同能力：
- PutObject 写 metadata。
- HeadObject 读 metadata。
- ListObjects 按 prefix 扫描。
- DeleteObject / DeleteObjects 删除具体 version object。

自定义 client 不支持 metadata 能力时，普通 CRUD 继续可用，lifecycle 能力返回明确 unsupported error。

### 5.3 COS
COS 与 S3-compatible 采用一致语义：
- 保存时写自定义 metadata。
- scan 时使用 Head 读取 metadata。
- cleanup 删除具体 version object。
- 默认后台 cleanup 只扫描新路径 `artifact/`。
- legacy 路径通过显式 root prefix 纳入治理。

## 6. 兼容性与失败语义
兼容性：
- 未配置 TTL 时，现有 artifact 行为不变。
- 旧 artifact 没有 lifecycle metadata 时仍可读取。
- 旧 artifact 默认不参与 TTL cleanup。
- 现有 `runner.WithArtifactService` 不需要修改。

失败语义：
- backend 不支持 lifecycle 能力时，返回稳定 unsupported error。
- backend 不支持某个 cleanup scope 时，返回稳定 scope unsupported error。
- 配置了 TTL 但无法写 lifecycle metadata 时，保存调用必须返回错误，不能静默保存成无 TTL artifact。
- cleanup 不做引用安全校验；业务启用 TTL 即接受过期 artifact 可能被删除。

## 7. 主要取舍
### 7.1 为什么不直接用对象存储 lifecycle
对象存储 lifecycle 更适合 bucket / prefix / tag 级批量治理，不适合表达：
- per artifact version `expires_at`。
- pinned 优先级。
- session / user scoped dry-run。
- deleted / skipped / failed 统计。
- framework artifact version 语义。

因此它可以作为运维兜底，但不能替代 J-1 主语义。

### 7.2 为什么需要 version allocator
当前 S3 / COS 的版本分配依赖 list 现存 versions 后 `max + 1`。一旦 cleanup 删除最高 version，后续保存可能复用 version。

sidecar allocator 的目标是保证：
- cleanup 不导致 version 复用。
- 历史 ref 不会读到未来新内容。
- 失败时允许 version gap，但不允许复用 version。

### 7.3 为什么后台 cleanup 要 root prefix
S3 当前没有统一 artifact 根前缀。如果后台 cleanup 默认从空 prefix 扫描，就可能扫整个 bucket。

J-1 选择显式 root prefix 作为第一版方案：
- 行为可控。
- 成本可解释。
- 不引入额外 lifecycle index 一致性问题。

高规模场景后续可考虑 lifecycle index 优化。

## 8. 验收口径
J-1 可发布验收：
- 新保存 artifact version 可以写入 lifecycle metadata。
- 可以查询 artifact version lifecycle metadata。
- service-level 默认 TTL 生效。
- per-save TTL / retention / pinned 覆盖生效。
- scan / dry-run 返回逐 artifact version 明细。
- cleanup 删除已过期且未 pinned 的 artifact version。
- cleanup 结果至少区分 deleted / skipped / failed。
- 后台 cleanup 可启停，且不重入。
- cleanup 删除 version 后，后续保存不复用该 version。
- `artifact/inmemory`、`artifact/s3`、`artifact/cos` 均覆盖核心语义。
- 对象存储后台 cleanup 有明确扫描边界，不默认扫描整个 bucket。
- 文档明确提示：J-1 cleanup 不保证引用安全，需要引用安全治理时使用 J-2。
