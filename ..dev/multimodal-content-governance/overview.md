# 多模态内容治理规划

## 1. 出发点

本规划的主旨是“多模态内容治理”，而不是单点修复 session 中存储 base64 的问题。

随着框架支持图片、音频、文件等非文本内容，inline 多模态数据可能进入 session、track、checkpoint、trace、debuglog 等多类存储。如果这些状态、事件、观测类存储不加区分地承载大块非文本内容，会带来存储膨胀、读写变慢、序列化成本升高、网络传输变重、日志/观测系统负担增加等问题。

因此，本规划希望在整体层面一次性梳理清楚仓库内多模态内容的流转和持久化边界，避免后续围绕不同存储面反复小修小补。

核心治理思路是：

- 状态、事件、观测类存储保存轻量引用、摘要和必要元信息。
- 大块非文本内容进入 artifact 等更适合的大对象存储。
- 运行时在需要时通过引用恢复内容。
- 已由业务方或 provider 管理的外部引用需要兼容，不强制重复托管。

## 2. 整体目标

本规划目标是提升框架内各类状态、事件、观测存储的性能与治理能力。

具体包括：

- 降低 session、track、checkpoint、trace、debuglog 等存储中 inline 多模态内容造成的膨胀。
- 减少大对象在 JSON 序列化、反序列化、网络传输、日志导出、观测上报中的重复成本。
- 明确 artifact 在框架中的定位：承载框架接收或产生的 inline 多模态大对象。
- 明确多模态内容在各类存储面中的引用、恢复、生命周期和兼容边界。
- 为后续生命周期治理、权限/审计/合规扩展提供结构基础，但不在当前阶段承诺完整合规实现。

## 3. 预期收益

### 3.1 存储性能收益

- 降低状态、事件、观测类存储承载大对象带来的体积膨胀。
- 降低 session history 读取、事件追加、分页查询、恢复上下文时的 payload 大小。
- 减少 DB/Redis/ClickHouse 等存储后端处理大块 JSON/base64 的压力。
- 降低一次性加载历史时的内存占用和 OOM 风险。

### 3.2 成本收益

- 避免把大对象放进更昂贵或不适合的大量状态存储、日志存储和观测存储。
- 减少重复持久化同一份多模态 bytes 的概率。
- 降低备份、导出、trace 上报、日志采集中的带宽和存储成本。

### 3.3 框架能力收益

- 建立统一的大对象治理边界：artifact 保存内容，其他存储保存引用。
- 让 future features 复用统一能力，例如多模态 tool result、AG-UI 多模态输入、code executor 产物、skill 输出等。
- 为生命周期、版本、追踪、删除、审计、访问控制等后续能力打基础。

### 3.4 业务收益

- 业务方传入图片、文件等内容时，不必担心框架 session 等存储被大对象撑大。
- 多模态业务增长时，成本和稳定性更可控。
- 历史对话、前端回放、调试和审计可以通过引用定位内容，而不是依赖散落在 JSON blob 中的 base64。

### 3.5 不夸大的收益

本规划不直接承诺：

- 替业务方管理所有源文件生命周期。
- 自动完成脱敏、权限、加密、审计等完整合规治理。
- 降低模型输入 token 或多模态推理成本。模型输入压缩是相邻方向，不放在本规划主线中。
- 对业务方或 provider 已经提供的 URL/file ID 进行强制重托管。

## 4. 仓库内存储面盘点

本规划要求盘点仓库内所有可能涉及多模态内容持久化的存储面。盘点不代表全部实现，但不应遗漏。

### 4.1 Session Events

`session.Events` 是对话历史主存储，会持久化 `event.Event`，其中包含 `model.Message` 和 `ContentParts`。

风险：

- `Image.Data`、`Audio.Data`、`File.Data` 会随 event JSON 序列化为 base64。
- path/base64 输入最终也可能变成 inline bytes 进入 session。
- 下一轮恢复上下文时，大对象会被重新读入内存。

规划判断：

- 纳入治理主路径。
- 应优先治理。

### 4.2 Session Tracks

`session.Tracks` 用于协议或前端事件流，例如 AG-UI track。

风险：

- AG-UI 用户消息 custom event 可能保存多模态输入 payload。
- 该链路走 `AppendTrackEvent`，不走普通 `AppendEvent`。
- 如果只治理 session events，AG-UI track 仍可能保留一份多模态 base64。

规划判断：

- AG-UI 是框架团队关注点，应纳入整体规划和盘点。
- 是否进入第一期实现另行评估。

### 4.3 Session State / StateDelta

`session.State` 和 `event.StateDelta` 是 `map[string][]byte`，理论上可保存任意 bytes。

风险：

- 某些工具、graph、业务扩展可能把大对象写进 state。
- state 会随 session 更新持久化，可能成为隐藏的大对象入口。

规划判断：

- 纳入盘点和治理原则。
- 需要进一步确认现有代码是否把多模态原文写入 state。

### 4.4 Session Summaries

summary 主要保存文本摘要。

风险：

- 通常不应保存多模态原文。
- 需要确认 summary 流程不会复制完整 base64 或大对象文本化结果。

规划判断：

- 纳入盘点，初步低风险。

### 4.5 Graph Checkpoint

graph checkpoint 可能保存图执行状态、消息快照或中间状态。

风险：

- 如果 checkpoint 保存完整 state/message，可能间接复制多模态 bytes。
- 多轮图执行或回放场景下，checkpoint 可能成为第二份长期存储。

规划判断：

- 纳入盘点和治理原则。
- 实现优先级后续再议。

### 4.6 Telemetry / Trace

telemetry 和 trace 可能记录 request、response、message 或 event。

风险：

- 如果观测属性中包含 base64，多模态内容会进入 trace/exporter。
- 观测系统通常不适合保存大对象原文。

规划判断：

- 纳入盘点和治理原则。
- 优先考虑引用化、截断、omit、drop 等策略。

### 4.7 Debuglog / Snapshot

debuglog 可能对 request、response、event 做 JSON snapshot。

风险：

- snapshot 若直接 marshal 原始结构，会保存完整多模态 bytes/base64。
- 调试日志容易被长期保留或外传。

规划判断：

- 纳入盘点和治理原则。
- 实现优先级后续再议。

### 4.8 Memory / Context Offload

memory 和 context offload 已存在外部化、摘要、替换等思路，主要面向文本或 tool result。

风险：

- 如果未来处理多模态 tool result，可能需要与 artifact 治理协同。
- 当前文本 offload 不等同于多模态大对象治理。

规划判断：

- 纳入盘点。
- 需明确与本规划边界：外存解决“存在哪里”，context compression 解决“给模型多少”。

### 4.9 Workspace / CodeExecutor / Skill Output

workspace 和 code executor 涉及文件输入、产物和 artifact 引用。

风险：

- 临时 workspace 文件本身不一定是问题。
- 若产物被写回 session/tool result/debuglog，则可能进入持久化路径。

规划判断：

- 纳入盘点。
- 区分临时文件系统与长期持久化存储。

### 4.10 Artifact Storage

artifact 是目标承载层，不是问题面。

规划判断：

- 用于承载框架接收或产生的 inline 多模态大对象。
- 需要定义引用、元信息、恢复、生命周期、错误处理等契约。

### 4.11 Vector Index / Pgvector

vector index 通常提取文本用于索引。

风险：

- 当前主要低风险，但需要确认不会保存多模态原文。

规划判断：

- 纳入盘点，初步低优先级。

## 5. 多模态触达路径盘点

需要覆盖所有多模态内容进入框架或由框架产生的路径。

### 5.1 用户消息

包括直接构造 `model.Message`、`ContentParts`、base64 helper、path helper 等。

规划判断：

- 纳入主路径。

### 5.2 RunOptions.Messages / Seed History

业务可能通过 `RunOptions.Messages` 传入已有历史，其中也可能包含多模态内容。

规划判断：

- 纳入主路径。

### 5.3 UserMessageRewriter

rewriter 可能把一个用户输入改写成多个消息，这些消息也可能包含多模态内容。

规划判断：

- 纳入主路径。

### 5.4 AG-UI Input

AG-UI 输入可能包含图片、音频、文件等内容，并进入 model message 或 track event。

规划判断：

- 纳入整体规划和盘点。
- 是否首期实现后续再定。

### 5.5 Tool Result

当前 tool result 主要偏文本，但主流模型已支持多模态 tool result，未来框架也可能支持。

规划判断：

- 纳入整体规划。
- 具体支持程度后续按 provider 和 model adapter 能力评估。

### 5.6 CodeExecutor / Skill Output

代码执行、技能运行可能产生文件，并保存为 artifact 或返回给模型/用户。

规划判断：

- 纳入盘点。
- 优先鼓励产物以 artifact 引用形式进入消息或 tool result。

### 5.7 业务方自存内容

业务方可能已经提供 URL、对象存储地址、provider file ID 或业务自己的文件 ID。

规划判断：

- 整体设计必须兼容业务方自存需求。
- session 不应保存多模态原文。
- 具体引用形式可以在实现阶段确认。
- 不强制把业务方已有外部引用重新托管到框架 artifact。

## 6. 治理原则

### 6.1 Inline 大对象应被治理

凡是以 bytes/base64/path 读入后形成的 inline 多模态内容，只要可能进入长期或半长期存储，就应纳入治理。

### 6.2 外部引用不强制重托管

URL、provider file ID、业务自有文件引用已经是外部引用。框架应兼容这些引用，不默认复制一份到 artifact。

### 6.3 状态/事件/观测存储保持轻量

session、track、checkpoint、trace、debuglog 等应优先保存引用、摘要和元信息，而非完整二进制内容。

### 6.4 Artifact 承载内容本体

artifact service 是框架内承载大对象的统一能力，应承接框架接收或产生的 inline 多模态内容。

### 6.5 外存必须可恢复

如果某个存储面将内容替换为引用，则需要明确恢复路径。恢复目标可能是：

- 模型调用
- 前端展示
- 工具执行
- 调试排查
- 审计导出

### 6.6 生命周期纳入规划

artifact 生命周期需要纳入整体规划。是否在首期实现自动清理另行评估。

### 6.7 合规作为未来兼容方向

设计上应避免阻碍未来权限、加密、审计、脱敏、删除等合规能力。但现阶段没有明确收益，不纳入实现目标。

### 6.8 输入压缩不纳入本规划主线

多模态输入压缩、摘要、OCR、caption 等属于“模型输入控制”方向。本规划主线是存储治理，不混入输入压缩实现。

## 7. 整体边界

### 7.1 纳入规划

- session event 中的多模态 inline data
- session track 中的多模态 payload，尤其 AG-UI
- session state/state delta 中潜在的大对象
- graph checkpoint 中潜在的消息或状态快照
- telemetry/trace 中潜在的 request/response/message/event 复制
- debuglog snapshot 中潜在的大对象复制
- tool result 多模态能力的未来扩展
- codeexecutor/skill output 与 artifact 的协同
- artifact 引用、恢复、元信息、生命周期原则
- 业务方自存内容的兼容策略

### 7.2 不纳入主线

- 模型输入压缩
- 完整合规治理实现
- 对业务方已有 URL/file ID 的强制重托管
- 对每个 DB backend 分别做重复多模态逻辑

### 7.3 待评估边界项

- 是否治理所有 `StateMap` 中的大 bytes
- checkpoint 具体外存策略
- telemetry/debuglog 是引用化、截断、omit 还是 drop
- AG-UI track 是否纳入首期
- artifact 生命周期是否首期实现自动清理

## 8. 优先级分层

优先级分层用于表达整体规划中的重要性，不等同于版本拆分。

### P0：核心主路径

- session events 中的多模态 inline data
- artifact 引用和元信息契约
- session replay 时的恢复能力
- 当前运行时与持久化视图分离

### P1：框架重点路径

- AG-UI track 多模态 payload
- debuglog/telemetry 中明确复制多模态的路径
- tool result 多模态的结构兼容
- artifact 生命周期原则和最小观测指标

### P2：后续治理扩展

- graph checkpoint 深度治理
- session state/state delta 大对象治理
- 历史数据迁移
- 完整 GC、审计、权限、加密、脱敏能力
- 更复杂的 dedupe、hash、版本清理策略

## 9. 后续拆期原则

后续可以从整体规划中拆出第一版、第二版等迭代，但拆分必须遵守以下原则：

- 每一期都必须是完整、可发布、可测试的 PR。
- 不能把一个不可用的半成品强拆成多个需求。
- 如果某个能力缺失会导致前一版不可用，则必须合并进同一版。
- 每期都要有明确用户可感知行为、默认策略和兼容说明。
- 盘点范围可以大于实现范围，但文档中必须说明未实现项的后续位置。

## 10. 已确认决策

- 需求主旨是多模态内容治理。
- 核心收益是提升框架内各类状态、事件、观测存储的性能与治理能力。
- 文档需要覆盖仓库内相关存储面的盘点，但不代表都要实现。
- 输入压缩不放在本规划中做。
- 生命周期纳入规划，但不一定作为初版实现。
- 设计上考虑未来合规扩展，但现阶段不纳入实现。
- 兼容业务方自存内容，不强制重托管业务已有外部引用。
- AG-UI 纳入整体规划和盘点，但不一定纳入第一期。
- checkpoint、telemetry、debuglog 等纳入盘点和治理原则，具体优先级再议。
- 文档定位是整体规划与边界说明。

## 11. 待确认问题

- 盘点中是否还有遗漏的存储面。
- 多模态治理是否需要覆盖 video 或通用 binary blob。
- 业务方自存引用在 session 中应保存哪些元信息。
- artifact 引用字段采用显式字段还是复用既有字段。
- artifact 生命周期与 session/user/app 生命周期的默认关系。
- telemetry/debuglog 的治理策略是引用化、截断、omit 还是 drop。
- checkpoint 中哪些状态必须可恢复，哪些可以只保留摘要。
- AG-UI track 是否进入第一期实现。
- 是否需要历史 session 迁移方案。
