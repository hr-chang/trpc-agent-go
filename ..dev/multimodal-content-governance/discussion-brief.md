# 多模态内容治理方案讨论稿

## 1. 背景
本次规划的主旨是“多模态内容治理”，不是单点修复 session 中存储 base64 的问题。

随着框架支持图片、音频、文件、video、PDF、通用 binary blob 等非文本内容，这些内容可能进入 session、track、state、checkpoint、telemetry、debuglog、evalset、workspace、artifact 等多类状态、事件、观测和资产存储。如果大块非文本内容被直接内联保存，会带来几个问题：
- 状态/事件存储膨胀，DB、Redis、ClickHouse、MongoDB 等读写压力上升。
- JSON 序列化、反序列化、网络传输、日志导出、观测上报成本变高。
- 历史 session replay、debug snapshot、eval case、checkpoint 恢复时更容易出现慢查询、内存放大或 OOM。
- 同一份多模态内容可能在 session、trace、debuglog、evalset 中被重复保存。

因此，需要从整体层面一次性梳理清楚：多模态内容从哪里进入框架，进入后如何流转，在哪些环节会被存储、外发或回放，以及哪些存储点需要治理。

## 2. 目标
目标是提升框架内各类状态、事件、观测存储的性能与治理能力。

核心方向：
- 大块非文本内容进入 artifact、业务对象存储、OpenClaw uploads 等更适合的大对象存储。
- session、track、checkpoint、trace、debuglog、evalset 等状态/观测/回放类存储保存轻量引用、摘要和必要元信息。
- 运行时在需要时通过引用恢复内容，保证模型调用、前端回放、调试、评测等场景可用。
- 兼容业务方自存和 provider file ref，不强制重托管已有外部引用。

本规划不覆盖模型输入压缩、完整合规治理实现，也不承诺替业务方管理所有源文件生命周期。

## 3. 入口盘点结论
当前入口盘点可认为无遗漏。盘点方法是从第一手多模态入口出发，而不是从存储点倒推。

已覆盖的主要入口包括：
- 用户消息、seed history、`UserMessageRewriter` 输出。
- `InjectedContextMessages` / `LateContextMessages` 等非 session 注入消息。
- AG-UI、OpenAI-compatible API、A2A Server 入站、A2A Agent 远端响应。
- OpenClaw Gateway 输入、URL fetch-to-bytes、`MEDIA:` / `MEDIA_DIR:` tool-result media。
- Tool Result、ClaudeCode `Read` tool result、MCP image result。
- Callback / Tool / CodeExecutor / Skill 输出。
- Workspace conversation-file dereference。
- Model / Provider Response、Graph State / Interrupt。

需要特别说明的几个结论：
- ClaudeCode `Read` 工具是已存在的内置风险点：读取图片/PDF 时会把 bytes 以 base64 放入 tool result JSON，随后进入 session events、telemetry/debuglog/eval recorder。
- Evaluation recorder 是派生记录面，不是第一手来源。
- 直接 session/track/state API 是公开写入面，不是独立内容来源。
- `StateMap` 不是单独入口，而是入口链路上的存储落点；治理应沿多模态入口链路展开。

## 4. 需要治理的存储面
治理范围覆盖以下存储面，但不代表都进入第一期实现：
- `session.Events`：核心主路径，当前最需要治理的长期/半长期对话状态。
- `session.Tracks`：AG-UI 等协议回放路径，可能独立保存多模态 payload。
- `session.State` / `StateDelta` / app state / user state：作为入口链路上的存储落点治理，不作为单独入口。
- graph checkpoint：可能保存完整 messages/state。
- telemetry / OTLP / Langfuse：可能外发 request、message、response、tool result 中的多模态内容。
- debuglog / ExecutionTrace：可能保存 request/response/event snapshot。
- evalset / eval result：可能把线上多模态 payload 录制为评测资产。
- OpenClaw uploads / debug recorder / workspace filesystem。
- artifact.Service：框架默认大对象承载层。

## 5. 核心设计结论
- 治理逻辑不应下沉到每个 DB backend 分别实现；DB backend 只负责存储已经治理过的 payload。
- session 主路径应区分 runtime view 和 persisted view：运行时保留模型调用需要的原始内容，持久化前替换为轻量引用和元信息。
- inline bytes / base64 / data URL 是主要治理对象；普通 URL、provider file ID、业务 host ref、业务自有对象存储引用不默认重托管。
- data URL 虽然位于 URL 字段，但本质是 inline base64，应按 inline 内容治理。
- artifact service 是框架内默认大对象承载层；业务方自存内容是并行的外部引用形态，需要兼容。
- 外存不是单向删除 bytes，必须同时定义 hydrate/replay 路径。
- telemetry、debuglog、checkpoint、eval 不一定第一期全部实现，但默认策略不能继续无约束复制完整多模态 payload。

## 6. 整体边界
纳入规划：
- 从所有多模态入口出发的存储路径盘点。
- session event、AG-UI track、state/state delta、checkpoint、telemetry、debuglog、evalset 等存储面的治理原则。
- artifact 引用、恢复、元信息、生命周期原则。
- 业务方自存内容的兼容策略。
- 历史 session 数据兼容。

不纳入主线：
- 模型输入压缩。
- 完整合规治理实现。
- 对业务方已有 URL/file ID/host ref 的强制重托管。
- 对每个 DB backend 分别做重复多模态逻辑。

## 7. 历史兼容要求
历史 session 兼容是框架层强约束，不是后续版本再补的能力。

框架会被业务从历史版本升级使用，因此每一版都必须支持历史框架版本已经写入的数据，包括历史 inline bytes/base64 和未来引用化数据。可以把历史数据迁移工具放到后续需求，但运行时读取、回放、hydrate 或兼容解析不能缺位；不能出现某一版发布后不支持历史落盘 DB 数据，等待下一版再补支持的情况。

## 8. 后续需求拆分建议
优先级分层只表达重要性，不直接等同于需求拆分。

P0 核心主路径：
- 用户消息、seed history、rewriter、AG-UI/A2A 输入进入 `session.Events` 的路径。
- `session.Events` 中 inline 多模态内容的持久化视图治理。
- artifact 引用和元信息契约。
- session replay / hydrate 能力。
- runtime view 与 persisted view 分离。

P1 框架重点路径：
- AG-UI track 多模态 payload。
- OpenAI-compatible data URL、A2A 远端响应、非 session 注入消息、ClaudeCode Read、MCP image result。
- OpenClaw URL fetch、MEDIA tool-result media、workspace dereference。
- callback/tool/codeexecutor/skill 产物与 artifact 的协同。
- telemetry/debuglog 中明确复制多模态的路径。

P2 后续治理扩展：
- graph checkpoint 深度治理。
- state/state delta 大对象治理。
- evaluation recorder / evalset / eval result 治理。
- 历史数据迁移工具。
- 完整 GC、审计、权限、加密、脱敏能力。

## 9. 需后续具体需求确认
以下内容不在整体规划阶段提前拍板，应在具体需求设计中确认：
- 业务方自存引用需要保存哪些元信息，以及字段位置。
- artifact 引用字段具体形式。
- artifact 生命周期与 session/user/app 生命周期的默认关系。
- telemetry/debuglog 的治理策略是引用化、截断、omit、drop，还是 debug 模式 opt-in 保存。
- checkpoint 中哪些状态必须完整可恢复，哪些可以只保存摘要或引用。
- AG-UI track、evaluation recorder/evalset 是否进入第一期。
- `StateMap` 是否需要大小阈值、key 策略、文档约束或框架内部写入约束。
