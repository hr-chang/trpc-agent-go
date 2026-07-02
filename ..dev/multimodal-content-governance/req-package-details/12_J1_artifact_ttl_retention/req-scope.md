# 需求范围：Artifact TTL / Retention 基础能力

## 1. 背景
当前 `artifact.Service` 已经提供基础的 artifact 保存、读取、列举、删除和版本列举能力，但 artifact 本体缺少生命周期元信息。

现有公开结构主要是：
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

当前 `runner.WithArtifactService(...)` 已经允许业务在 runner 层配置统一的 artifact service；工具、回调、codeexecutor、workspace 输出以及 session externalization 都可以通过这个 service 保存 artifact。因此 TTL / retention 不应被设计成 session externalization 的私有能力，而应作为 `artifact.Service` 层的独立基础能力。

A 包开始把 session 多模态内容保存成 artifact 后，artifact 的使用面会显著扩大，也让存储膨胀、保留期限和清理可见性问题更早暴露。但这只是 J-1 的一个重要使用场景，不是 J-1 的前置依赖。J-1 做完后，任何通过 runner 配置的 artifact service 都应可以直接使用 TTL / retention 基础能力。

需要特别说明：S3、COS、MinIO、R2 等对象存储通常不具备与 MongoDB TTL index 完全一致的 per-record 过期语义。因此 J-1 的需求目标应定义为：对业务呈现统一的 artifact TTL / retention 行为，而不是要求某一种固定底层实现方式。

TTL / retention 只是 artifact 基础能力。它可以在 ArtifactService 自身范围内删除已经过期的 artifact version，但它本身不能判断 artifact 是否仍被 session、tool result、workspace output、debug/eval 等引用。因此 J-1 解决“表达、扫描与按 TTL 清理”，J-2 再解决“跨系统引用关系与安全删除治理”。

## 2. 目标
J-1 要为 artifact 层建立最小 TTL / retention 基础能力。

核心目标：
- 完整性 P0：J-1 不能只提供局部 TTL 能力；启用 TTL 后必须形成“配置、保存、过期、扫描、后台 cleanup、结果可见”的完整闭环。
- 独立能力：TTL / retention 能力归属于 artifact service，不依赖 session externalization。
- 默认 TTL：artifact service 可以配置 service-level 默认 TTL；未配置时行为与现有版本一致。
- 单次覆盖：单次保存 artifact 时可以覆盖 service-level 默认 TTL，支持同一 artifact service 下混合临时产物与长期产物。
- 元信息表达：artifact version 可以记录过期时间、保留策略和 pinned/no-expire 语义。
- 零值兼容：未设置 TTL / retention 时，现有 artifact 行为不变。
- 可查询：业务或后续治理任务可以读取 artifact 的生命周期元信息。
- 可扫描：提供过期扫描 / dry-run 能力，帮助业务看到哪些 artifact 已过期或即将过期。
- 可清理：启用 TTL 后，ArtifactService 可以通过后台 cleanup 删除已过期且未 pinned 的 artifact version。
- 对象存储全覆盖：本期直接支持现有对象存储型 backend，包括 `artifact/cos` 与 `artifact/s3` 所覆盖的 S3-compatible storage（AWS S3、MinIO、R2 等）。

## 3. 核心范围
### 3.1 覆盖对象
J-1 面向 artifact service 管理的 artifact，不限定来源。

纳入：
- session externalization / A 包保存的 session `ContentRef` artifact。
- workspace / tool / skill / codeexecutor 显式保存的 artifact。
- 业务通过 artifact service 直接保存的 artifact。

不按来源区分策略；J-1 只提供 artifact 层基础元信息与扫描能力。

J-1 与 session externalization 的关系：
- J-1 不依赖 externalization，也不要求 A.2 先完成。
- externalization 后续可以作为调用方，在保存多模态 artifact 时传入默认 TTL / retention 策略。
- J-1 不在 externalization 内部维护私有 TTL 状态，避免只能覆盖 session 多模态外存这一种来源。
- runner 现有 `WithArtifactService` 入口保持不变，业务只需配置支持 lifecycle 能力的 artifact service 即可使用 J-1。

### 3.2 生命周期元信息
J-1 需要定义可演进的 artifact lifecycle metadata。

建议最小字段：
- `expires_at`：
    - artifact version 的过期时间。
    - 零值表示未设置过期时间。
- `retention_class` 或 `retention_policy`：
    - 业务可读的保留策略名称。
    - 只表达策略，不在 J-1 中强制解释全部业务语义。
- `pinned` / `no_expire`：
    - 表达默认不应被过期扫描选中。
    - 用于长期保留、迁移中间态、debug/eval 保留等场景。
- `created_at` / `updated_at`：
    - 如果 backend 当前缺失创建时间，J-1 应评估是否作为 lifecycle metadata 一并补齐。

### 3.3 粒度
已确认：TTL / retention 应绑定到 artifact version，而不是只绑定 artifact name。

原因：
- 当前 artifact 是版本化对象，`SaveArtifact` 每次保存会返回 version。
- session `ContentRef` 固定引用 `artifact://<name>@<version>`。
- 同名不同版本可能有不同来源、大小、保留周期和引用关系。

### 3.4 能力入口与兼容性
J-1 不应破坏现有 `artifact.Service` 调用方，但需要提供面向业务的 TTL / retention 使用入口。

需求层面需要覆盖：
- artifact service 可以配置默认 TTL。
- 保存 artifact 时可以使用默认 TTL，也可以单次覆盖 TTL / retention。
- 可以查询 artifact version 的生命周期信息。
- 可以扫描已过期 artifact。
- 可以对已过期 artifact 执行 dry-run 或真实 cleanup。

约束：
- 现有 `SaveArtifact(ctx, info, filename, artifact)` 调用不设置 TTL 时，行为与现在一致。
- 现有 `LoadArtifact` 的内容读取语义不应被破坏。
- backend 不支持完整扫描时，应通过 capability 或错误语义明确表达。
- 不应要求所有使用方通过 session externalization 才能设置 TTL；tool / callback / codeexecutor 等直接保存 artifact 的路径也应有可用入口。
- cleanup 必须支持后台定时清理，同时提供 dry-run 或显式扫描能力用于观测和运维。

具体 API 形态归 `tech-design.md` 决定，不在需求范围中强制指定。

### 3.5 过期扫描与 cleanup
J-1 提供过期扫描和 cleanup 能力。

扫描结果至少应包含：
- session/app/user 范围信息。
- artifact name。
- version。
- expires_at。
- retention / pinned 信息。
- 是否已过期。
- backend 可解释的原因或限制。

cleanup 语义：
- cleanup 只处理已过期且未 pinned / no-expire 的 artifact version。
- cleanup 必须支持 dry-run，dry-run 不删除任何 artifact。
- cleanup 在非 dry-run 模式下会调用 backend 删除过期 artifact。
- TTL 到期不要求“到点立即删除”，但 cleanup 触发后必须能删除符合条件的 artifact。
- cleanup 必须包含后台定时清理能力；cleanup interval、关闭方式和显式触发 API 由技术设计决定。
- 业务启用 TTL 代表接受 ArtifactService 按 TTL 删除 artifact 的语义；J-1 cleanup 不因 artifact 仍被 session/tool/workspace 等引用而自动跳过。

扫描范围需要按 backend 能力确定：
- session-scoped scan。
- user-scoped scan。
- app/global scan。

首期必须覆盖 artifact 现有语义中的 session-scoped 与 user-scoped artifact。app/global scan 不作为 J-1 硬验收，可根据 backend 成本和能力作为 capability 明示或后续增强。

现有对象存储型 backend 必须在上述核心范围内可用：
- `artifact/cos`。
- `artifact/s3`，并覆盖 AWS S3、MinIO、R2 等 S3-compatible storage 的共同能力集。

不同对象存储的底层实现方式由技术设计决定；需求范围只要求对业务呈现一致、可解释的 TTL / scan / cleanup 行为。

## 4. 已确认决策
默认行为：
- 默认不设置 TTL。
- 未配置 TTL 时不启用 cleanup 删除。
- 未实现 lifecycle metadata 的旧 artifact 必须可继续读取。
- 业务配置入口以相对 TTL duration 为主；`expires_at` 作为内部标准生命周期字段，并可在 lifecycle 查询结果中对外可见。
- TTL 到期不等于立即删除；它代表 artifact 进入 cleanup 可删除状态。
- 启用 TTL 后，后台 cleanup 可以删除过期且未 pinned 的 artifact。
- 单次保存显式 TTL / retention 优先于 service-level 默认 TTL。
- `pinned` / `no_expire` 优先于 TTL；即使存在 `expires_at`，也不应被默认 TTL cleanup 删除。

删除边界：
- J-1 可以根据 TTL 执行 ArtifactService 范围内的真实删除。
- J-1 不判断 artifact 是否仍被引用。
- J-1 不联动 session 删除。
- J-1 不承诺引用安全；业务启用 TTL 即接受过期 artifact 可能被删除，即使仍存在历史引用。
- J-1 只按 artifact version 的 TTL / pinned / retention metadata 判断是否可清理。
- J-2 负责在引用关系、orphan 判断和更复杂 pinned / retention 策略基础上做治理级安全删除。

兼容边界：
- 新增能力必须保持向后兼容。
- 不要求所有 artifact backend 在第一版支持完全一致的扫描范围，但必须明确能力差异。

## 5. 非目标
J-1 不做：
- 不做 artifact 引用关系追踪。
- 不做 orphan 判断。
- 不做 session 删除联动。
- 不做跨系统引用安全校验。
- 不做复杂后台清理调度、失败重试和告警闭环。
- 不把 TTL 做成 session externalization 的私有实现。
- 不做 provider file id 生命周期管理。
- 不做 workspace 文件 GC。
- 不做完整权限、审计、加密、脱敏。

这些能力分别归 J-2、I、D 或未来合规治理。

## 6. 验收口径
### 6.1 元信息
- 可以为新保存的 artifact version 设置 `expires_at` / retention / pinned 信息。
- 未设置 lifecycle metadata 的 artifact 行为与现有版本一致。
- metadata 与 artifact version 绑定，能区分同名不同版本。

### 6.2 读取与列举
- 可以读取 artifact lifecycle metadata。
- 可以列举 artifact versions 时获得 lifecycle metadata，或通过明确的 metadata API 查询。
- 旧 artifact 没有 metadata 时返回稳定的零值语义。

### 6.3 dry-run 扫描
- 可以在支持的范围内扫描已过期 artifact。
- dry-run 输出稳定、可解释。
- dry-run 不删除任何 artifact。
- backend 不支持某种扫描范围时，有明确错误或 capability 表达。

### 6.4 cleanup 删除
- 可以对已过期 artifact 执行非 dry-run cleanup。
- cleanup 会删除已过期且未 pinned / no-expire 的 artifact version。
- cleanup 后对应 artifact version 不再能被正常 `LoadArtifact` 读取。
- cleanup 删除某个 artifact version 后，后续保存不得复用该 version，避免历史 `artifact://...@version` 引用读到新内容。
- cleanup 不处理未过期 artifact、pinned / no-expire artifact、未设置 lifecycle metadata 的旧 artifact。
- cleanup 的删除范围、删除数量和跳过原因应可观测。
- 启用 TTL 后后台 cleanup 能自动执行清理；显式 dry-run / scan 能看到同一批候选对象。
- 显式 scan / dry-run 必须返回逐 artifact version 明细；cleanup 失败必须可见，至少能区分 deleted / skipped / failed，并提供可解释原因。
- 用户文档必须明确提示：启用 TTL 后，过期 artifact 可能被删除，即使历史 session/tool/workspace 中仍存在引用；需要引用安全治理时使用 J-2 能力。
- 对象存储后台 cleanup 必须有明确扫描边界，不应默认扫描整个 bucket。

### 6.5 兼容性
- 现有 artifact 保存/读取/删除调用方不需要修改。
- runner 现有 `WithArtifactService` 配置入口不需要修改。
- 现有 artifact backend 测试继续通过。
- 至少覆盖 `artifact/inmemory`。
- 本期必须直接支持现有对象存储型 backend：`artifact/cos` 与 `artifact/s3`。
- `artifact/s3` 的实现必须落在 S3-compatible storage 的共同能力上，覆盖 AWS S3、MinIO、R2 等使用场景。

## 7. 推迟到 J-2 的事项
- 引用关系追踪。
- orphan 判定。
- session 删除联动清理。
- expired + orphan 的治理级安全删除。
- 清理失败重试和告警。
- 基于 retention class 的复杂业务策略解释。

## 8. 待确认问题
需求范围层面暂无待确认问题。

技术实现细节统一由 `tech-design.md` 承接，不作为需求范围约束。
