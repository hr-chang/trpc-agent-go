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
- `InjectedContextMessages` / `LateContextMessages` 等非 session 注入消息。
- AG-UI 输入、OpenAI-compatible API 输入、A2A Server 入站、A2A Agent 远端响应。
- OpenClaw Gateway 输入、OpenClaw URL fetch、OpenClaw MEDIA tool-result media。
- Tool Result、ClaudeCode Read Tool Result、MCP Tool Image Result、Callback / Tool / CodeExecutor / Skill 输出。
- Workspace conversation-file dereference、Model / Provider Response、Graph State / Interrupt 输入。

当前识别出的主要治理存储点包括：
- `session.Events`、`session.Tracks`、`session.State` / `StateDelta`。
- graph checkpoint、telemetry / OTLP、Langfuse、debuglog、ExecutionTrace。
- evalset、eval result / benchmark output。
- OpenClaw uploads、OpenClaw debug recorder、workspace filesystem。
- artifact.Service、memory store、pgvector/text index。

该盘点的定位是完整性依据：它覆盖规划讨论范围，但不代表所有路径都进入首期实现。

本轮复核后的判断：
- 当前入口盘点可认为无遗漏。
- 最后一项已补齐的现存遗漏是 ClaudeCode `Read` 工具会将图片/PDF bytes 以 base64 放入 tool result JSON。
- 其他已补充的容易忽略来源包括：非 session 注入消息、OpenClaw URL fetch-to-bytes、OpenClaw MEDIA tool-result media、workspaceinput 对 host path / artifact ref / provider file ID 的 dereference。
- `agent.CallbackContext.SaveArtifact` 代表回调/工具直接生成 artifact，是合理的产物入口。
- `evaluation recorder` 是派生记录面，不是第一手来源；直接 `session.Service` / `Session` 写入 API 是公开写入面，也不是独立内容来源。
- knowledge OCR / document readers 当前更像知识库文本抽取链路，不默认进入会话多模态存储；作为边界项保留，不列为主入口。
- telemetry、debuglog、checkpoint、evalset 是派生存储或外发点，不是第一手来源，但必须纳入治理原则。

## 5. 核心设计结论摘要
- 治理逻辑不应下沉到每个 DB backend 分别实现；DB backend 应只负责存储已经治理过的 payload。
- session 主路径采用 runtime view 与 persisted view 分离：运行时保留模型调用需要的原始内容，持久化前替换为轻量引用和元信息。
- inline bytes / base64 / data URL 是主要治理对象；普通 URL、provider file ID、业务 host ref、业务自有对象存储引用不默认重托管。
- data URL 虽然位于 URL 字段，但本质是 inline base64，大对象场景应按 inline 内容治理。
- artifact service 是框架内默认大对象承载层；业务方自存内容是并行的外部引用形态，需要兼容。
- 外存不是单向删除 bytes，必须同时定义 hydrate/replay 路径，保证历史 session、前端回放、调试、评测等必要场景可恢复。
- 观测、debug、checkpoint、eval 不一定首期全部实现，但默认策略应避免继续无约束复制完整多模态 payload。

## 6. 治理原则
### 6.1 Inline 大对象应被治理
凡是以 bytes、base64、data URL、path 读入后形成的 inline 多模态内容，只要可能进入长期或半长期存储，就应纳入治理。

### 6.2 外部引用不强制重托管
URL、provider file ID、业务自有文件引用、OpenClaw host ref 已经是外部引用。框架应兼容这些引用，不默认复制一份到 artifact。

但 data URL 不是普通外部引用。它虽然位于 URL 字段，本质仍是 inline base64 大对象，应按 inline 内容治理。

### 6.3 状态/事件/观测存储保持轻量
session、track、checkpoint、trace、debuglog、evalset 等应优先保存引用、摘要和元信息，而非完整二进制内容。

### 6.4 Artifact 承载内容本体
artifact service 是框架内承载大对象的统一能力，应承接框架接收或产生的 inline 多模态内容。

业务方已有对象存储或 provider file ref 可以作为另一类外部引用，治理方案需要兼容，但不要求强制迁移到框架 artifact。

### 6.5 外存必须可恢复
如果某个存储面将内容替换为引用，则需要明确恢复路径。恢复目标可能是：
- 模型调用。
- 前端展示。
- 工具执行。
- 调试排查。
- 审计导出。
- evaluation replay。

### 6.6 生命周期纳入规划
artifact、uploads、workspace、debug recorder、evalset 等都有生命周期问题。是否在首期实现自动清理另行评估，但整体规划必须覆盖。

### 6.7 合规作为未来兼容方向
设计上应避免阻碍未来权限、加密、审计、脱敏、删除等合规能力。但现阶段没有明确收益，不纳入实现目标。

### 6.8 输入压缩不纳入本规划主线
多模态输入压缩、摘要、OCR、caption 等属于“模型输入控制”方向。本规划主线是存储治理，不混入输入压缩实现。

## 7. 整体边界
### 7.1 纳入规划
- 从所有多模态入口出发的存储路径盘点。
- session event 中的多模态 inline data。
- session track 中的多模态 payload，尤其 AG-UI。
- session state/state delta 中潜在的大对象。
- graph checkpoint 中潜在的消息或状态快照。
- telemetry/trace 中潜在的 request/response/message/event 复制。
- debuglog/ExecutionTrace 中潜在的大对象复制。
- eval recorder/evalset/eval result 中潜在的多模态资产。
- OpenClaw uploads/debug recorder 与 session 的多模态关系。
- OpenClaw URL fetch-to-bytes 与 MEDIA tool-result media。
- OpenAI-compatible data URL 的 inline 内容风险。
- A2A 入站和远端响应中的多模态内容。
- 非 session 注入消息进入 provider/telemetry/debug/eval 的路径。
- ClaudeCode `Read` 工具 inline base64 图片/PDF 的 tool result 路径。
- MCP image result 等工具结果多模态能力。
- codeexecutor/skill output 与 artifact 的协同。
- callback/tool 直接保存 artifact 的产物链路。
- workspaceinput 对 host path、artifact ref、provider file ID 的 dereference。
- 直接 session event/track/state 写入的治理约束。
- artifact 引用、恢复、元信息、生命周期原则。
- 业务方自存内容的兼容策略。

### 7.2 不纳入主线
- 模型输入压缩。
- 完整合规治理实现。
- 对业务方已有 URL/file ID/host ref 的强制重托管。
- 对每个 DB backend 分别做重复多模态逻辑。

## 8. 优先级分层
优先级分层用于表达整体规划中的重要性，不等同于版本拆分。

### P0：核心主路径
- 用户消息、seed history、rewriter、AG-UI/A2A 输入进入 `session.Events` 的路径。
- `session.Events` 中的多模态 inline data。
- artifact 引用和元信息契约。
- session replay 时的恢复能力。
- 当前运行时与持久化视图分离。

### P1：框架重点路径
- AG-UI track 多模态 payload。
- OpenAI-compatible data URL、A2A 远端响应、非 session 注入消息、ClaudeCode Read、MCP image result 等入口治理。
- OpenClaw URL fetch、MEDIA tool-result media、workspace dereference 等字节物化路径。
- callback/tool/codeexecutor/skill 产物与 artifact 的协同。
- 直接 session/track/state 写入的文档约束和必要防护。
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
- 入口盘点当前可认为无遗漏。
- 历史 session 兼容是框架层强约束：每一版都必须支持历史框架版本已写入的数据，不能把兼容性延后到后续版本。

## 11. 后续需讨论问题与当前阶段判断
### 11.1 Video 与通用 binary blob
当前阶段判断：规划层面覆盖 video 和通用 binary blob，但不在整体规划阶段定义专门实现策略。

通用 binary blob 指无法或不需要在框架层进一步理解语义的任意非文本 bytes，例如 `application/octet-stream`、压缩包、未知格式文件、二进制中间产物、业务私有文件、PDF 等。它和 image/audio/video 的区别不是大小，而是框架无法按专门内容类型理解，只能作为“文件/bytes 对象”治理。

Video 可能从以下入口引入：
- 用户消息：`AddFileData` / `AddFilePath` 或 `File.Data` 携带 video mime type。
- A2A：`FileWithBytes` / `FileWithURI` 携带 video mime type。
- OpenClaw Gateway：已有 `PartTypeVideo` / uploads 形态。
- OpenAI-compatible 或其他协议入口：可能通过 file URL、data URL、file ref 表达。
- Tool / CodeExecutor / Skill / Callback：生成视频文件或 video artifact ref。
- Workspace dereference：host path、artifact ref、provider file ID 指向视频文件时会物化为 workspace bytes。
- 直接 session/track/state 写入：业务代码可直接把视频 bytes/base64 写入通用 payload。

通用 binary blob 可能从以下入口引入：
- 用户消息：`File.Data`、`AddFileData`、`AddFilePath`。
- A2A：非 image/audio 的 `FileWithBytes` 默认进入 `File.Data`。
- OpenClaw Gateway：普通 file/data URL/URL fetch/uploads。
- ClaudeCode Read：PDF 会以 base64 写入 tool result。
- Tool / CodeExecutor / Skill / Callback：任意输出文件、artifact、workspace 文件。
- Workspace dereference：`host://`、`artifact://`、provider file ID。
- `StateMap` / `StateDelta`：业务或 graph state 可保存任意 `[]byte`。

### 11.2 业务方自存引用
当前阶段判断：只确认整体原则，即框架需要兼容业务方自存，不强制重托管，且 session 等状态存储不应保存多模态本体。业务方自存引用需要保存哪些元信息、引用 URI 形态、字段位置等细节，放到具体需求设计中确认。

### 11.3 Artifact 引用字段与生命周期
当前阶段判断：只确认 artifact 是框架默认大对象承载层，并且外存引用必须可恢复。artifact 引用字段具体形式、是否复用既有字段、artifact 生命周期与 session/user/app 生命周期的默认关系，均放到具体需求设计中确认。

### 11.4 Telemetry / Debuglog 治理策略
当前阶段判断：只确认 telemetry/debuglog 属于治理范围，且不应默认无约束复制完整 inline 多模态 payload。具体是引用化、截断、omit、drop，还是 debug 模式 opt-in 保存 blob，放到具体观测/调试需求中确认。

### 11.5 Checkpoint 恢复边界
当前阶段判断：只确认 checkpoint 属于治理范围，且不应无约束保存完整 inline bytes。checkpoint 中哪些状态必须完整可恢复、哪些可以只保存摘要或引用，放到 graph checkpoint 子设计中确认。

### 11.6 `StateMap` 作为存储落点
当前阶段判断：`StateMap` 不是独立的第一手多模态入口，而是某些入口链路上的存储落点或写入面。治理应沿多模态入口链路展开：当用户消息、tool result、graph state、业务扩展等把多模态内容写入 `StateMap` / `StateDelta` 时，这条链路需要治理；不应把问题理解为无差别治理所有 `StateMap`。

`StateMap` 是 `map[string][]byte`，覆盖 session state、app state、user state，以及 event `StateDelta` 中的状态变更。它的值没有内容类型约束，可能是 JSON，也可能是任意 bytes。

`StateMap` 中可能出现的多模态或大对象形态包括：
- 原始 bytes：业务直接把图片、音频、视频、文件、压缩包、PDF 等放入某个 state key。
- base64 字符串：业务为了 JSON 兼容，把多模态 bytes 编码成 base64 后放入 state。
- JSON 包装对象：state value 是 JSON，内部字段包含 data URL、base64、`model.Message`、`ContentParts`、tool result、文件内容等。
- 引用对象：state value 只保存 `artifact://`、`host://`、provider file ID、业务 URL 等引用和元信息。
- graph state snapshot：graph/subgraph 可能把最终状态序列化进 `StateDelta`，如果状态中包含 message、tool output、文件内容，就会间接进入 state。
- track index / framework metadata：这类通常很小，不属于多模态治理对象。

难点在于 `StateMap` 是通用 KV，框架无法仅凭类型判断哪些 key 是业务必要状态、哪些是大对象误用。全量拦截或自动外存可能破坏业务自定义状态语义。因此整体规划阶段只确认原则：`StateMap` 不作为单独入口治理；不鼓励任何入口链路把大对象本体写入 `StateMap`；框架内部写入的 state 应优先保存引用；是否做大小阈值、白名单/黑名单、按 key 策略、或仅文档约束，放到具体需求中确认。

### 11.7 AG-UI Track 与 Evaluation
当前阶段判断：AG-UI track、evaluation recorder/evalset 是否进入第一期，不在整体规划阶段决定。本文只确认它们属于盘点和治理范围，具体优先级在需求拆分阶段确定。

### 11.8 历史 session 兼容与迁移
当前阶段判断：历史 session 兼容是必要要求，而且不是“后续版本补齐”的能力。

项目定位是框架层，会有业务从历史版本升级而来。因此每一版都必须支持历史框架版本已经写入的数据，包括历史 inline bytes/base64 和未来引用化数据。可以把历史数据迁移工具放到后续需求，但运行时读取、回放、hydrate 或兼容解析不能缺位；不能出现某一版发布后不支持历史落盘 DB 数据，等待下一版再补支持的情况。
