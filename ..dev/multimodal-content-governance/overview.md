# 多模态内容治理规划

## 1. 出发点
本规划的主旨是“多模态内容治理”，不是单点修复 session 中存储 base64 的问题。

随着框架支持图片、音频、文件等非文本内容，inline 多模态数据可能进入 session、track、checkpoint、trace、debuglog、evalset 等多类状态、事件、观测和资产存储。如果这些存储不加区分地承载大块非文本内容，会带来存储膨胀、读写变慢、序列化成本升高、网络传输变重、日志/观测系统负担增加等问题。

因此，本规划希望从整体层面一次性梳理清楚：多模态内容从哪里进入框架，进入后如何流转，在哪些环节会发生存储、外发或回放，以及哪些存储点需要治理。这样可以避免后续围绕 session、AG-UI、trace、checkpoint、eval 等不同面反复小修小补。

核心治理思路：
- 状态、事件、观测、回放、评测类存储保存轻量引用、摘要和必要元信息。
- 大块非文本内容进入 artifact、业务对象存储、OpenClaw uploads 等更适合的大对象存储。
- 运行时在需要时通过引用恢复内容。
- 已由业务方或 provider 管理的外部引用需要兼容，不强制重复托管。

## 2. 整体目标
本规划目标是提升框架内各类状态、事件、观测存储的性能与治理能力。

具体包括：
- 降低 session、track、checkpoint、trace、debuglog、evalset 等存储中 inline 多模态内容造成的膨胀。
- 减少大对象在 JSON 序列化、反序列化、网络传输、日志导出、观测上报、评测录制中的重复成本。
- 明确 artifact 在框架中的定位：承载框架接收或产生的 inline 多模态大对象。
- 明确多模态内容在各类存储面中的引用、恢复、生命周期和兼容边界。
- 为后续生命周期治理、权限、审计、合规扩展提供结构基础，但不在当前阶段承诺完整合规实现。

## 3. 预期收益
### 3.1 存储性能收益
- 降低状态、事件、观测类存储承载大对象带来的体积膨胀。
- 降低 session history 读取、事件追加、分页查询、恢复上下文时的 payload 大小。
- 减少 DB、Redis、ClickHouse、MongoDB、trace backend、日志系统处理大块 JSON/base64 的压力。
- 降低一次性加载历史、checkpoint、debug snapshot、eval case 时的内存占用和 OOM 风险。

### 3.2 成本收益
- 避免把大对象放进更昂贵或不适合的大量状态存储、日志存储、观测存储和评测资产中。
- 减少重复持久化同一份多模态 bytes 的概率。
- 降低备份、导出、trace 上报、日志采集、评测资产同步中的带宽和存储成本。

### 3.3 框架能力收益
- 建立统一的大对象治理边界：artifact 或业务对象存储保存内容，其他存储保存引用。
- 让 future features 复用统一能力，例如多模态 tool result、AG-UI 多模态输入、code executor 产物、skill 输出、provider 多模态响应等。
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
- 对业务方或 provider 已经提供的 URL、file ID、host ref 进行强制重托管。

## 4. 存储路径盘点
详细盘点已拆分到 [多模态存储路径盘点](./storage-path-inventory.md)。

盘点方法从第一手多模态入口出发，而不是从存储点倒推。每条链路按以下层次描述：
```text
入口/来源
  -> 框架内标准表示
  -> 运行时消费链路
  -> 存储/外发/回放点
  -> 链路终止点
  -> 治理判断
```

当前盘点覆盖的主要入口包括：
- 用户消息、`RunOptions.Messages` / seed history、`UserMessageRewriter` 输出。
- AG-UI 输入、OpenAI-compatible API 输入、A2A Server 入站、A2A Agent 远端响应。
- OpenClaw Gateway 输入、Tool Result、MCP Tool Image Result。
- CodeExecutor / Skill 输出、Model / Provider Response。
- Graph State / Interrupt 输入、Evaluation Recorder。

当前识别出的主要治理存储点包括：
- `session.Events`、`session.Tracks`、`session.State` / `StateDelta`。
- graph checkpoint、telemetry / OTLP、Langfuse、debuglog、ExecutionTrace。
- evalset、eval result / benchmark output。
- OpenClaw uploads、OpenClaw debug recorder、workspace filesystem。
- artifact.Service、memory store、pgvector/text index。

该盘点的定位是完整性依据：它覆盖规划讨论范围，但不代表所有路径都进入首期实现。

## 5. 治理原则
### 5.1 Inline 大对象应被治理
凡是以 bytes、base64、data URL、path 读入后形成的 inline 多模态内容，只要可能进入长期或半长期存储，就应纳入治理。

### 5.2 外部引用不强制重托管
URL、provider file ID、业务自有文件引用、OpenClaw host ref 已经是外部引用。框架应兼容这些引用，不默认复制一份到 artifact。

但 data URL 不是普通外部引用。它虽然位于 URL 字段，本质仍是 inline base64 大对象，应按 inline 内容治理。

### 5.3 状态/事件/观测存储保持轻量
session、track、checkpoint、trace、debuglog、evalset 等应优先保存引用、摘要和元信息，而非完整二进制内容。

### 5.4 Artifact 承载内容本体
artifact service 是框架内承载大对象的统一能力，应承接框架接收或产生的 inline 多模态内容。

业务方已有对象存储或 provider file ref 可以作为另一类外部引用，治理方案需要兼容，但不要求强制迁移到框架 artifact。

### 5.5 外存必须可恢复
如果某个存储面将内容替换为引用，则需要明确恢复路径。恢复目标可能是：
- 模型调用。
- 前端展示。
- 工具执行。
- 调试排查。
- 审计导出。
- evaluation replay。

### 5.6 生命周期纳入规划
artifact、uploads、workspace、debug recorder、evalset 等都有生命周期问题。是否在首期实现自动清理另行评估，但整体规划必须覆盖。

### 5.7 合规作为未来兼容方向
设计上应避免阻碍未来权限、加密、审计、脱敏、删除等合规能力。但现阶段没有明确收益，不纳入实现目标。

### 5.8 输入压缩不纳入本规划主线
多模态输入压缩、摘要、OCR、caption 等属于“模型输入控制”方向。本规划主线是存储治理，不混入输入压缩实现。

## 6. 整体边界
### 6.1 纳入规划
- 从所有多模态入口出发的存储路径盘点。
- session event 中的多模态 inline data。
- session track 中的多模态 payload，尤其 AG-UI。
- session state/state delta 中潜在的大对象。
- graph checkpoint 中潜在的消息或状态快照。
- telemetry/trace 中潜在的 request/response/message/event 复制。
- debuglog/ExecutionTrace 中潜在的大对象复制。
- eval recorder/evalset/eval result 中潜在的多模态资产。
- OpenClaw uploads/debug recorder 与 session 的多模态关系。
- OpenAI-compatible data URL 的 inline 内容风险。
- A2A 入站和远端响应中的多模态内容。
- MCP image result 等工具结果多模态能力。
- codeexecutor/skill output 与 artifact 的协同。
- artifact 引用、恢复、元信息、生命周期原则。
- 业务方自存内容的兼容策略。

### 6.2 不纳入主线
- 模型输入压缩。
- 完整合规治理实现。
- 对业务方已有 URL/file ID/host ref 的强制重托管。
- 对每个 DB backend 分别做重复多模态逻辑。

### 6.3 待评估边界项
- 是否治理所有 `StateMap` 中的大 bytes。
- checkpoint 具体外存策略。
- telemetry/debuglog 是引用化、截断、omit 还是 drop。
- AG-UI track 是否纳入首期。
- artifact 生命周期是否首期实现自动清理。
- eval recorder 是否需要在首期加治理。
- video 或通用 binary blob 是否在第一版纳入。

## 7. 优先级分层
优先级分层用于表达整体规划中的重要性，不等同于版本拆分。

### P0：核心主路径
- 用户消息、seed history、rewriter、AG-UI/A2A 输入进入 `session.Events` 的路径。
- `session.Events` 中的多模态 inline data。
- artifact 引用和元信息契约。
- session replay 时的恢复能力。
- 当前运行时与持久化视图分离。

### P1：框架重点路径
- AG-UI track 多模态 payload。
- OpenAI-compatible data URL、A2A 远端响应、MCP image result 等入口治理。
- telemetry / Langfuse 中明确复制多模态的路径。
- debuglog / ExecutionTrace 中明确复制多模态的路径。
- OpenClaw uploads/debug recorder。
- tool result 多模态的结构兼容。
- artifact 生命周期原则和最小观测指标。

### P2：后续治理扩展
- graph checkpoint 深度治理。
- session state/state delta 大对象治理。
- evaluation recorder / evalset / eval result 治理。
- 历史数据迁移。
- 完整 GC、审计、权限、加密、脱敏能力。
- 更复杂的 dedupe、hash、版本清理策略。

## 8. 后续拆期原则
后续可以从整体规划中拆出第一版、第二版等迭代，但拆分必须遵守以下原则：
- 每一期都必须是完整、可发布、可测试的 PR。
- 不能把一个不可用的半成品强拆成多个需求。
- 如果某个能力缺失会导致前一版不可用，则必须合并进同一版。
- 每期都要有明确用户可感知行为、默认策略和兼容说明。
- 盘点范围可以大于实现范围，但文档中必须说明未实现项的后续位置。

## 9. 已确认决策
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

## 10. 待确认问题
- 入口盘点中是否还有遗漏的第一手多模态来源。
- 多模态治理是否需要覆盖 video 或通用 binary blob。
- 业务方自存引用在 session 中应保存哪些元信息。
- artifact 引用字段采用显式字段还是复用既有字段。
- artifact 生命周期与 session/user/app 生命周期的默认关系。
- telemetry/debuglog 的治理策略是引用化、截断、omit 还是 drop。
- checkpoint 中哪些状态必须可恢复，哪些可以只保留摘要。
- AG-UI track 是否进入第一期实现。
- evaluation recorder/evalset 是否需要进入较早期实现。
- 是否需要历史 session 迁移方案。
