# 需求范围：Session 多模态外存最小闭环

## 1. 背景
`session.Events` 是框架对话历史和运行状态恢复的核心存储路径。当前多模态内容可以通过标准 `model.Message.ContentParts` 进入 session event，其中 image/audio/file 都可能携带 inline bytes 或 data URL。

如果这些内容被无约束写入 session backend，会带来存储膨胀、读写变慢、序列化成本升高、历史读取放大等问题。

## 2. 目标
需求包 A 要建立 session 主路径的最小治理闭环。

核心目标：
- 写入减重：新写入 `session.Events` 的标准多模态消息不再无约束保存大块 inline 内容。
- 运行不变：当前轮模型调用不受 persisted event 减重影响。
- 读取兼容：历史 inline session、新引用化 session、混合 session 都能读取和继续对话。
- 能力复用：为后续 AG-UI track、tool result、trace/debug、checkpoint、eval 治理复用统一引用和 hydrate 能力。

## 3. 核心范围
### 3.1 覆盖写入面
本需求覆盖所有写入 `session.Events` 的主要入口面。

框架主路径：
- runner 当前轮用户消息。
- seed history / message rewriter 输出。
- assistant / provider response event。

适配与扩展路径：
- server adapter 写入 session event。
- team runtime 写入 session event。
- 业务直接调用 `session.Service.AppendEvent`。

### 3.2 治理对象
本需求不按内容来源区分是否业务自定义，只按载体结构区分。

纳入治理：
- 进入 `session.Events` 的标准 `model.Message.ContentParts`。
- `ContentParts` 中的 `Image.Data`、`Audio.Data`、`File.Data`。
- 标准消息结构中 URL 字段承载的 data URL。
- 已经被框架或应用转换成标准 `ContentParts` 的工具/应用派生多模态消息。

不纳入治理：
- tool result JSON 内部的 base64/data URL/blob 字段，归需求包 C。
- 任意业务自定义 JSON/string/metadata 中的 inline blob。
- `StateDelta` / `StateMap` 中的大对象，归需求包 F 或业务约束。
- AG-UI track 独立 payload，归需求包 B。
- telemetry/debuglog/eval/checkpoint 复制面，归后续需求包。

### 3.3 小对象是否外存
已确认：A 包不实现按大小阈值治理。

判断：
- 不建议无条件外存所有多模态。
- 体积很小的 inline 内容可以保留原样，避免引入 artifact 写入、hydrate 和引用管理成本。
- A 包设计需要保留未来支持阈值治理的兼容空间。
- 首期不提供阈值配置和阈值判断；后续有业务明确诉求时再做。

这意味着首期只需要明确“开启治理后如何外存标准 ContentParts”，不在本需求中解决“小对象保留 inline、大对象外存”的精细策略。

## 4. 已确认决策
> 这一节记录 A 包首期已经收敛的产品和技术边界。

治理边界：
- 插入位置：治理发生在 `AppendEvent` 进入具体 session backend 之前。
- 视图分离：治理动作是构造 persisted event，不修改 runtime event。
- 结构优先：A 包覆盖所有 `session.Events` 写入面，但只治理标准消息结构中的多模态内容。
- 来源无关：A 不按来源区分是否业务自定义；只要载体是标准 `ContentParts`，就属于 A。
- 不做深扫：A 不做任意 JSON 深扫；tool result JSON 内部 inline blob 由需求包 C 处理。

默认策略：
- 默认关闭：旧版本升级业务不应在未配置 artifact 和治理开关时自动改变行为。
- 配置入口：业务配置入口采用 runner option。
- 配置归属：首期对外配置类型归属 `runner` 包，不为了内部治理实现过早公开独立配置包。
- 配置演进：首期使用 config struct，哪怕当前只有 `Enabled` 字段，也不使用单 bool option，避免未来扩展时破坏 API。
- 实现核心：实现核心采用 session service decorator，治理发生在 `AppendEvent` 进入具体 backend 前。
- 读取兼容：A 包首期 `GetSession` 默认 hydrate，保持业务可见行为与历史 inline session 一致；未来再提供 without-hydrate 优化入口。
- 读取视图：hydrate 采用 copy-on-write，只影响返回给调用方的 session view，不把 bytes 写回 persisted event。

引用表达：
- 统一表达：persisted event 采用统一 internal ref/metadata，不把 `artifact://` 默认塞进 provider URL / provider file id 字段。
- 承载方式：在 `model.ContentPart` 上增加明确的统一 `ContentRef` 字段，不分散放入 `Image`、`Audio`、`File` 各自结构。
- 版本语义：首期 `ContentRef` 不强制加 `schema_version`，缺省版本即 v1。
- 命名规则：artifact name 采用 `sessionpart_<unix-ms>_<sha256-16>_<uuid>.<ext>`；`uuid` 由治理层通过 `uuid.NewString()` 生成，owner 信息放 metadata。
- provider 边界：`session.Events` 可以保存 internal artifact ref；进入 provider adapter 前，不允许存在 unresolved internal artifact ref。
- 请求构造：模型请求构造层必须完成 hydrate 或显式转换，失败则返回错误。
- API 边界：hydrate helper 首期仅作为框架内部能力，不对框架外公开，但需要放在可测试的内部 helper 中，供框架内部链路复用和单测覆盖。

## 5. 非目标
其他存储面：
- 不治理 AG-UI track 独立 payload。
- 不治理 tool result JSON 内部不结构化 blob。
- 不治理 telemetry/debuglog/checkpoint/evalset 的全部复制面。

高级治理能力：
- 不提供历史数据批量迁移工具。
- 不做完整 artifact GC、权限、审计、加密、脱敏。
- 不递归扫描业务自定义 JSON payload。
- 不做 provider 文件上传优化，也不缓存 provider file id。hydrate 后是否上传到 provider 属于后续性能优化能力。

## 6. 验收口径
### 6.1 写入
- 治理开启后，新写入 session event 中的标准 `ContentParts` inline bytes/data URL 不再直接落入 session backend。
- 治理关闭时，保持现有 inline 行为。
- 普通 URL、provider file ID、host ref、业务外部引用不被默认重托管。
- 多个 session backend 行为一致。

### 6.2 运行时
- 当前轮模型调用不受 persisted event 减重影响。
- runtime event 不因持久化治理被修改。

### 6.3 读取与恢复
- 历史 inline session 可读取、可继续对话。
- 新引用化 session 可读取、可继续对话。
- 混合 session 可读取、可继续对话。
- hydrate 失败有明确错误语义，不静默丢内容。

### 6.4 失败语义
- 治理开启但 artifact service 不可用时，不能静默丢内容。
- artifact 保存失败时，不能写入“bytes 已清空但 ref 不可用”的损坏 event。
- 结合仓库现有 artifact 调用惯例，默认倾向 fail closed：返回错误，不追加损坏 event。
- 多 part 保存时，若部分 artifact 已保存但 event 最终未追加，首期接受短期 orphan artifact；应提交对应 artifact 的 best-effort 删除请求，但 cleanup 不作为写入正确性的依赖。

## 7. 推迟到未来迭代的事项
以下事项已纳入整体规划，但不作为 A 包首期交付范围。

策略优化：
- 小对象阈值策略：首期不做按大小、类型或 data URL 长度的阈值判断；仅保留未来扩展空间。
- hydrate 性能优化：首期不提供 without-hydrate 读取入口；未来可增加 persisted view / lazy hydrate / message-event 粒度优化。
- hydrate API 公开：首期不对框架外公开 hydrate API；后续业务明确需要时再评估。
- provider 文件优化：首期不做 hydrate 后异步上传 provider，也不缓存 provider file id。

生命周期治理：
- 完整 artifact 生命周期：首期不做完整 GC、权限、审计、加密、脱敏和生命周期绑定。
- orphan artifact 治理：首期接受短期 orphan artifact；不做完整 orphan 判定、反引用索引或后台自动清理任务。
- 历史数据迁移工具：首期不提供历史 inline session 的批量迁移工具，只保证历史读取和继续对话兼容。

运行策略与观测：
- fail open 策略：首期不提供 artifact 保存失败后保留 inline 并继续写入的兼容开关。
- 治理统计指标：首期不接正式 metric；最多保留内部 result/summary，便于测试和后续接 telemetry。

其他需求包：
- 其他存储面治理：AG-UI track、tool result JSON、telemetry/debuglog/checkpoint/evalset 由后续需求包处理。

## 8. 待确认问题
截至当前讨论，A 包需求范围层暂无阻塞性待确认问题。
