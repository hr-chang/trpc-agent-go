# 技术设计：Session 多模态外存最小闭环

## 1. 文档定位
本文是需求包 A 的执行技术方案，配套需求范围见 `req-scope.md`。

本文只描述技术实现边界、链路、数据表示、失败语义和测试策略。需求范围变更优先修改 `req-scope.md`，技术实现再按需求范围同步调整。

## 2. 方案目标
需求包 A 解决 session 主路径的核心问题：写入 `session.Events` 时，不再无约束保存 inline 多模态大对象，同时保证模型调用、继续对话、历史读取和基础回放仍然可用。

本包是后续 AG-UI track、tool result、trace/debug、checkpoint、eval 等治理的基础包。它需要先把统一引用、外存、hydrate 和历史兼容能力建立起来。

## 3. 当前问题
### 3.1 多模态内容会直接进入 session event
直接来源：
- 用户消息可以携带 `model.ContentParts`。
- `ContentParts` 中的 image/audio/file 都可能包含 raw bytes。
- `AddImageData`、`AddAudioData`、`AddFileData` 会直接把 bytes 放入消息结构。
- `AddImageFilePath`、`AddAudioFilePath`、`AddFilePath` 会读取本地文件，再写入对应的 `Data` 字段。

派生来源：
- seed history。
- message rewriter。
- 协议 adapter。
- 工具或应用派生消息。

### 3.2 session backend 当前会存到治理前 payload
`session.Service.AppendEvent` 是公开写入面，runner、server adapter、team runtime 和业务直接调用都可能走到这里。如果只在某一个 runner append 点处理，会漏掉其他写入面；如果在每个 DB backend 中处理，又会造成重复实现和行为不一致。

### 3.3 运行时和持久化需求不同
- 运行时：
    - 模型请求构造需要原始 bytes 或 provider 可接受的引用。
    - 当前轮调用不能因为外存而丢内容。
- 持久化：
    - session event 不应长期保存大块 bytes/base64/data URL。
    - 存储中应保存轻量引用、摘要和必要元信息。
- 读取：
    - 首期 `GetSession` 默认 hydrate，保持业务可见行为与历史 inline session 一致。
    - 新引用化 session、历史 inline session、混合 session 都必须可用。

## 4. 设计原则
### 4.1 统一治理，不下沉到 DB backend
治理逻辑应位于 session backend 之上。DB backend 只负责保存已经治理过的 event payload，不关心多模态外存细节。

### 4.2 runtime view 与 persisted view 分离
- runtime view 保留当前运行需要的原始内容。
- persisted view 在写入 session 前把大对象替换为引用。
- 两者不能互相污染：持久化减重不能影响当前模型调用，`GetSession` 默认 hydrate 也不能把 bytes 写回 persisted event。

### 4.3 外存必须可恢复
外存不是单向删除 bytes。任何被替换成引用的内容，都需要有明确 hydrate 路径，至少支持继续对话和基础回放。

### 4.4 历史兼容是硬约束
新版本必须支持：
- 历史 inline session。
- 新引用化 session。
- 同一个 session 中同时存在 inline 和 ref 的混合数据。

### 4.5 按载体结构治理，不按来源治理
A 不按来源区分是否业务自定义。只要进入 `session.Events` 的内容载体是标准 `model.Message.ContentParts`，就属于本包治理对象。

反过来，即使内容来自框架内部，如果只是 tool result JSON、业务自定义 JSON、metadata、`StateDelta` 等非标准消息结构，也不在 A 包做深度治理。

## 5. 治理对象
### 5.1 首期治理对象
结构化字段：
- `model.ContentPart.Image.Data`
- `model.ContentPart.Audio.Data`
- `model.ContentPart.File.Data`

可识别 inline 表达：
- 标准消息 URL 字段中的 data URL。
- 已进入结构化消息层的 inline bytes/base64。

### 5.2 默认不重托管对象
- 普通 URL。
- provider file ID。
- host ref。
- 业务自有对象存储引用。
- 已经是 `artifact://` 的引用。

这些对象本身已经是外部引用。首期只需要兼容其元信息和恢复语义，不默认复制一份到 artifact。

### 5.3 暂不纳入本包的对象
后续需求包处理：
- tool result JSON 内部不结构化 base64，由需求包 C 处理。
- AG-UI track 独立 payload，由需求包 B 处理。
- telemetry/debuglog/checkpoint/evalset 的复制面，由后续需求包处理。

业务自定义内容：
- 所有业务自定义 `StateMap` 大对象，不在本包做全量扫描。
- 任意业务自定义 JSON/string/metadata 中的 inline blob，不在本包递归扫描。

## 6. 核心方案
### 6.1 引入 session persisted view 构造层
在 session backend 之上引入统一治理层，负责把 event 从 runtime view 转换成 persisted view。

治理发生在 `AppendEvent` 进入具体 session backend 之前。技术语义不是“全量扫描 event”，而是“遍历 event 中标准消息结构的 `ContentParts`，按规则构造 persisted event”。

形态对比：
- session service decorator：
    - 包装现有 `session.Service`。
    - 拦截 `AppendEvent`，写入前构造 persisted event。
    - 优点是覆盖直接 session 写入面。
- shared persist helper：
    - runner、adapter、team runtime 写入前统一调用。
    - 优点是依赖注入简单。
    - 缺点是容易漏掉直接 `AppendEvent` 调用。
- 推荐方向：
    > 对外是统一治理能力，对内复用独立 helper。
    - 以 session service decorator 为主。
    - 内部复用独立 governance helper，避免逻辑绑死在 session service。

### 6.2 治理层输入输出
输入：
- `context.Context`
- `session.Session` 或 session key 信息
- `event.Event`
- governance config
- `artifact.Service`

输出：
- persisted event
- artifact 写入结果
- 内部治理 result/summary
- 明确错误

关键要求：
- 对象所有权：
    - 不修改 runtime event 原对象。
    - persisted event 在需要替换内容时使用 clone 后的事件。
    - 如果遍历后没有任何内容需要替换，可以直接复用原 event，避免不必要的深拷贝。
- 失败处理：
    - 多个 content part 独立治理。
    - 单个 part 保存失败时按 fail closed 处理，不追加损坏 event。

### 6.3 copy-on-write clone
“发现需要替换时才 clone event”指 copy-on-write：
- 先只读遍历 event 中的标准 `ContentParts`。
- 如果没有 part 命中外存规则，直接把原 event 交给 backend。
- 如果某个 part 需要清空 inline data 或写入 artifact ref，再 clone event，并只修改 clone 后的 persisted event。
- runtime event 始终不被修改。

这样可以降低普通纯文本、小对象或外部引用消息的额外成本。

### 6.4 内容替换策略
对需要外存的 content part：
1. 提取 bytes、mime、文件名、格式、大小等元信息。
2. 判断治理开关是否开启。
3. 治理关闭时保留原样。
4. 治理开启时保存到 `artifact.Service`。
5. 在 persisted view 中清空 inline bytes/base64。
6. 写入统一 internal ref 和必要 metadata。

建议 persisted 表达保留：
- 内容信息：
    - 内容类型：image/audio/file。
    - mime type 或 format。
    - 原始文件名或展示名。
    - size。
- 引用信息：
    - artifact name。
    - artifact version。
    - internal artifact ref。
- 恢复辅助：
    - 是否从 data URL 转换而来。
    - 必要的 provider 相关字段，例如 image detail。

## 7. 建议数据表示
### 7.1 artifact URI
调研结论：
- 仓库已有 `artifact://` 作为内部文件引用 scheme。
- `internal/fileref` 能解析 `artifact://<name>@<version>`。
- `codeexecutor.ParseArtifactRef` 已支持从 `name@version` 拆出 artifact name 和版本。
- `workspace_save_artifact` 已把保存结果表达为 `artifact://<saved_as>@<version>`。

因此首期建议复用既有 URI 风格：
```text
artifact://<name>@<version>
```

说明：
- `name` 对应 `artifact.Service.SaveArtifact` 的 filename。
- `version` 对应返回的 revision ID。
- 必须固定 version，不建议 persisted event 依赖 latest，避免后续同名 artifact 新版本影响历史回放。

### 7.2 persisted content part
已确认：长期设计采用统一 internal ref/metadata。

不采用的默认方案：
- 不默认把 `artifact://...` 写入 `Image.URL`。
- 不默认把 `artifact://...` 写入 `File.URL`。
- 不把 OpenAI `file_id` 等 provider file ref 和框架 artifact ref 混用。

原因：
- `artifact://...` 是框架内部 persisted ref，不是 provider 可直接访问 URL。
- OpenAI adapter 会把 `Image.URL` 直接传给 provider，语义上不适合承载内部 ref。
- OpenAI `file_id` 是 provider 侧文件 ID，不等同于框架 artifact ref。
- Eino 的多模态结构也倾向区分 URL、Base64Data、MIMEType 和 Extra/metadata。

建议 persisted 表达：
- content part 原有语义字段保留必要非二进制信息。
- inline bytes/data URL 被清空。
- 新增或挂载统一 internal ref/metadata，用于记录 artifact 位置和恢复信息。

建议 metadata 至少包含：
- 引用字段：
    - `artifact_ref`：`artifact://<name>@<version>`。
    - `artifact_name`。
    - `artifact_version`。
- 内容字段：
    - `mime_type` 或 format。
    - `original_name`。
    - `size_bytes`。
    - `sha256`。
    - `from_data_url`。
- owner 字段：
    - `event_key`。
    - `message_index`。
    - `part_index`。
    - `request_id`。
- provider 辅助字段：
    - provider 相关非二进制参数，例如 image detail。

首期 `ContentRef` 不强制加 `schema_version`。读取时没有 `schema_version` 的 ref 视为 v1；未来如果出现不兼容语义，再显式增加 `schema_version`。

### 7.3 provider 字段与 internal ref 的边界
provider 输入字段只表达 provider 可消费内容：
- HTTP/HTTPS URL。
- provider file id。
- base64/data URL。
- file_data。

internal ref 只用于框架内部 persisted view：
- `artifact://...`
- `workspace://...`
- `host://...`

进入 provider adapter 前，internal ref 必须已经 hydrate 或显式转换。provider adapter 不应承担 session artifact 存储语义。

## 8. 写入链路
### 8.1 主流程
```text
runtime event
  -> traverse standard ContentParts
  -> decide keep inline or externalize
  -> clone only if replacement is needed
  -> save inline objects to artifact
  -> replace inline data with internal refs in persisted event
  -> append persisted event to session backend
```

### 8.2 需要覆盖的写入面
- runner 当前轮用户消息持久化。
- seed history / rewriter 输出持久化。
- assistant / provider response event。
- server adapter 写入 session event。
- team runtime 写入 session event。
- 业务直接调用 `session.Service.AppendEvent`。

### 8.3 为什么不能只改 runner
runner 中存在多处 `AppendEvent` 调用，而且仓库内还存在非 runner 写入面。只改某个 runner 分支会导致治理不完整，最终仍可能有 inline 多模态进入 session backend。

### 8.4 为什么不改每个 DB backend
DB backend 只负责存储，不应理解多模态治理策略。否则每个 backend 都要处理 artifact、开关、失败语义、hydrate 兼容，长期维护成本高且行为容易不一致。

## 9. 读取与 hydrate 链路
### 9.1 默认读取策略
首期 `GetSession` 默认 hydrate，保持业务可见行为与历史 inline session 一致。

原因：
- 旧版本中 `GetSession` 返回的是完整 inline session，业务代码可能直接读取 bytes。
- 如果启用外存后默认返回 ref，业务升级框架时可能需要适配大量读取代码。
- 默认 hydrate 可以把外存能力约束在持久化层内部，对业务保持更接近旧版本的心智。

代价：
- 首期 `GetSession` 的读取性能收益有限，大 session 仍可能触发 artifact load。
- artifact backend 故障可能影响读取完整 session。
- 很多只需要 metadata/ref 的消费方仍会承担 hydrate 成本。

后续优化方向：
- 增加 without-hydrate 读取入口。
- 增加 persisted view / lazy hydrate / message-event 粒度 hydrate。
- 对模型请求构造、前端回放、调试和评测做按需 hydrate。

### 9.2 建议 hydrate 触发点
首期默认：
- `GetSession` 默认 hydrate，保持业务可见行为与历史 inline session 一致。

内部必需：
- 模型请求构造：
    - 需要真实 bytes 或 provider 可接受格式时按需 hydrate。

后续优化：
- 前端回放：
    - 可按需 hydrate，或返回 ref/摘要让前端决定。
- 调试和评测：
    - 显式请求完整内容时 hydrate。

硬约束：
- `session.Events` 可以保存 internal artifact ref。
- 进入 provider adapter 前，不允许存在 unresolved internal artifact ref。
- 模型请求构造层必须完成 hydrate 或显式转换。
- hydrate 或转换失败时，模型调用返回明确错误。

不纳入本包：
- 是否把 hydrate 后的 bytes 异步上传给 provider。
- 是否缓存 provider file id。
- 是否按 provider 能力选择 file_id / file_data / URL 的性能优化策略。

### 9.3 hydrate API 形态
建议提供独立 helper，而不是把能力藏在 session backend 内部。

候选 API：
```go
HydrateMessage(ctx, sessionInfo, msg, opts) (model.Message, error)
HydrateEvent(ctx, sessionInfo, evt, opts) (*event.Event, error)
HydrateSession(ctx, sess, opts) (*session.Session, error)
```

首期至少需要：
- hydrate 单条 message。
- hydrate 单个 event。
- 模型请求构造链路可复用。

## 10. Artifact 命名与上下文
### 10.1 session 信息
`artifact.Service` 需要 `artifact.SessionInfo`：
- `AppName`
- `UserID`
- `SessionID`

治理层需要从 session 或 key 中稳定得到这些信息。

### 10.2 artifact name 建议
建议 name 不直接使用用户文件名作为唯一键，应生成稳定且避免冲突的名字。

调研注意：
- `artifact.Service` 使用 `SessionInfo + filename` 定位 artifact。
- S3 实现当前会拒绝包含 `/` 的 filename。
- 部分工具路径已有 `out/site.zip` 这类 ref 表达，但 A 包作为底层 session 治理能力，应优先选择各实现都更容易接受的保守命名。

已确认：首期 artifact name 使用不含路径分隔符的稳定名字。

```text
sessionpart_<uuid>_<sha256-16>.<ext>
```

说明：
- 唯一性：
    - `uuid` 由治理层通过 `uuid.NewString()` 生成，用于 artifact object id。
    - 每个 content part 独立生成 filename，避免同 filename 多 version 承担 part 区分职责。
- 可调试：
    - `sha256-16` 是内容 sha256 的前 16 位，用于调试和弱校验，不承担唯一性主责。
    - `ext` 只用于可读性，真实恢复依赖 metadata 中的 mime type / format。
- owner 信息：
    - `event_key`、`request_id`、`message_index`、`part_index` 等 owner 信息放 metadata，不塞进 filename。

命名原则：
- 不使用用户原始文件名作为唯一键。
- 不依赖 `/` 表达层级。
- 保留原始文件名作为元信息，而不是 artifact name 的唯一来源。

### 10.3 hash 与去重
首期不要求做全局 dedupe。

建议保留 hash 元信息：
- 便于调试。
- 便于后续迁移。
- 便于未来 dedupe。

## 11. 开关与阈值扩展
### 11.1 默认开关
已确认：多模态外存默认关闭。

原因：
- 旧版本升级业务默认没有该能力，不应自动改变落盘行为。
- 该能力依赖 artifact 配置，不是所有业务都已开启 artifact。
- 默认关闭能降低框架升级风险，避免未配置 artifact 时引入新错误路径。

生效条件：
- 业务显式开启 session 多模态外存。
- runner/session 治理层能拿到可用 `artifact.Service`。
- 当前 event 中存在标准 `ContentParts` inline 多模态内容。

### 11.2 配置入口与治理位置
已确认：业务配置入口采用 runner option；实现核心采用 session service decorator。

建议 API 形态：
```go
runner.WithSessionMultimodalExternalization(sessionmm.Config{
    Enabled: true,
})
```

治理真正发生的位置：
```text
runner / adapter / direct caller
  -> session.Service.AppendEvent
  -> session multimodal governance decorator
  -> concrete session backend
```

位置选择的原因：
- 覆盖完整：
    - `AppendEvent` 是 `session.Events` 的统一公开写入面。
    - runner 内部存在多处 `AppendEvent` 调用，decorator 能避免遗漏分支。
    - 业务直接调用 `session.Service.AppendEvent` 时，只要使用被包装的 service，也能获得一致治理。
- 职责清晰：
    - concrete DB backend 不需要理解 artifact、hydrate、开关和失败语义。
    - runner option 只负责装配 decorator 和传递配置，不承载具体治理逻辑。

如果业务绕过 runner 自行持有 session service，应提供独立 constructor/decorator，使业务可以显式包装自己的 session service。

### 11.3 阈值能力
已确认：A 包首期不实现阈值治理。

设计要求：
- 保留未来加入阈值策略的扩展点。
- 当前实现不提供按大小、内容类型、data URL 长度的阈值配置。
- 治理开启后，标准 `ContentParts` 中命中的 inline 多模态内容按统一规则外存。

后续如果业务提出小对象保留诉求，可以扩展：
- 按 bytes 大小。
- 按内容类型。
- 按 data URL 长度。
- 按 session/app 级配置。

## 12. 失败语义
### 12.1 artifact service 未配置
不能静默丢内容。

调研结论：
- `CallbackContext.SaveArtifact` 缺少 service 或 session 时直接返回错误。
- `codeexecutor.SaveArtifactHelper` 缺少 service 时直接返回错误。
- `workspace_save_artifact` 缺少 artifact service/session info 时直接返回错误。

建议策略：
- 治理开启但 artifact service 未配置：返回明确错误。
- 治理关闭：保持现有 inline 行为。
- 首期不默认提供 warn 后保留 inline 的隐式降级。

### 12.2 artifact 保存失败
不能写入“已清空 bytes 但 ref 不可用”的 event。

调研结论：
- 仓库惯例：
    - artifact save helper 和工具调用方普遍把保存失败作为 error 返回。
    - codeexecutor 输出保存失败会返回错误，不自动退回 inline。
    - session `AppendEvent` 错误在主路径上会向调用方传播。
- ADK 参考：
    - Google ADK 的 artifact service 是单 artifact 保存粒度，`save_artifact` 返回 filename scope 下的 version。
    - ADK 未提供跨多个 artifact 的批量事务或自动 rollback。
    - ADK 的 `delete_artifact` / GCS 实现按 filename 删除 artifact 的所有版本，不是删除单个 version。
- 仓库删除粒度：
    - 本仓库 `artifact.Service.DeleteArtifact` 也是按 filename 删除。
    - S3/COS 实现会删除该 filename 下的所有版本。

建议策略：
- 基础语义：
    - 默认 fail closed：返回错误，不追加 persisted event。
    - 不写入“artifact 保存失败但 bytes 已清空”的损坏 event。
    - 首期不提供 fail open 兼容开关，避免 artifact 保存失败时悄悄退回 inline，削弱治理效果。
- 多 part 语义：
    - 如果前几个 part 已保存成功、后续 part 保存失败，则本次 event 不追加。
    - 已保存成功的 artifact 首期允许成为短期 orphan。
- cleanup 语义：
    - cleanup 指失败后尝试删除本次已保存成功、但未被 event 引用的 artifact。
    - 首期 cleanup 不作为正确性依赖。
    - 可不做，或仅在成本很低时做 best-effort cleanup。
    - cleanup 失败不能覆盖原始保存错误。
    - 每个 part 使用独立 filename，避免 cleanup 删除同一 filename 下其他有效版本。

### 12.3 hydrate 失败
hydrate 失败必须显式返回错误，不能把内容静默当成空内容。

前端或调试场景可以把错误转换成可解释提示；模型请求场景通常应中断本轮调用或返回明确错误。

## 13. 历史兼容
### 13.1 历史 inline session
读取历史 session 时仍应识别原有 inline bytes/base64，并允许继续对话。

### 13.2 新引用化 session
首期 `GetSession` 默认 hydrate 新引用化 session，保持业务可见行为与历史 inline session 一致。

同时，模型请求构造必须能按需恢复 internal ref，避免 unresolved internal ref 进入 provider adapter。

### 13.3 混合 session
同一个 session 内可能同时存在：
- 历史 inline event。
- 新 persisted ref event。
- 治理关闭期间写入的 inline event。

所有读取、继续对话和基础回放逻辑都需要兼容混合形态。

## 14. 迁移与发布策略
### 14.1 建议分步实现
- Step 1：定义治理契约和引用表达。
- Step 2：实现 event/message 标准结构遍历与 persisted view 构造。
- Step 3：接入 artifact save。
- Step 4：接入 session 写入边界。
- Step 5：实现 hydrate helper。
- Step 6：接入模型请求构造处按需 hydrate。
- Step 7：补历史兼容和混合数据测试。

### 14.2 PR 拆分原则
需求包 A 可以拆多个实现 PR，但不能拆成多个独立发布版本。

如果中间 PR 不能形成完整闭环，需要满足：
- 默认行为不变。
- 或通过内部开关隐藏。
- 或只引入不改变行为的 helper 和测试。

## 15. 测试口径
### 15.1 单元测试
- 治理命中：
    - image/audio/file data 被替换为 artifact ref。
    - data URL 被识别为 inline 内容。
    - 治理开启时标准 `ContentParts` inline 多模态被外存。
- 治理跳过：
    - 普通 URL、provider file ID、host ref 不被重托管。
    - 治理关闭时保持现有 inline 行为。
- ref 表达：
    - persisted event 使用统一 internal ref/metadata，不把 `artifact://` 写入 provider URL/file id 字段。
    - artifact name 符合 `sessionpart_<uuid>_<sha256-16>.<ext>`。
    - 缺省 `schema_version` 的 `ContentRef` 被识别为 v1。
- 对象与失败：
    - 未命中治理规则时不强制 clone event。
    - 命中治理规则时 runtime event 不被修改。
    - artifact save 失败不产生损坏 event。
    - hydrate 失败返回明确错误。
    - provider adapter 前存在 unresolved internal ref 时返回明确错误。

### 15.2 集成测试
- 至少覆盖一个持久化 session backend。
- 新写入 session 不保存被治理的大块 inline bytes。
- `GetSession` 默认 hydrate 新引用化 session，业务可见行为与历史 inline session 一致。
- 当前轮模型调用不受 persisted view 影响。
- 新引用化 session 可继续对话。
- 历史 inline session 可继续对话。
- 混合 session 可继续对话。

### 15.3 回归测试
- runner 主路径写入。
- seed history 写入。
- rewriter 输出写入。
- assistant response 写入。
- 直接 `session.Service.AppendEvent` 写入。

## 16. 待确认问题
截至当前讨论，A 包技术设计层暂无阻塞性待确认问题。
