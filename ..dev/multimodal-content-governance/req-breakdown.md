# 多模态内容治理需求拆解

## 1. 文档定位
本文基于 `overview.md` 和 `storage-path-inventory.md`，将多模态内容治理规划拆成可讨论、可排期、可发布的需求包。

本文只描述需求层面的目标、范围、边界、依赖和验收口径。涉及具体代码链路、字段、函数、现存风险点和实现证据的内容，统一放在 `requirement-package-details/` 下的 per-package 技术细节文档。

## 2. 拆解原则
核心原则：
- 发布单元：
    - 每个需求包都应是一版可发布的 PR/MR，不能只是定义、设计、文档或半成品能力。
    - 一个需求包可以包含多个实现点，但发布后必须形成完整闭环：能写、能读、能回放、能兼容历史、能测试。
- 横向能力：
    - 纯契约定义、测试、文档、升级说明不单独作为需求包，应内嵌到对应可发布需求中。
    - 历史 session 兼容是每个相关需求包的硬约束，不能留到下一版再补。
- 范围控制：
    - 每个需求包要明确不做什么，避免范围不断扩张。
    - 代码细节只作为需求设计依据，不直接堆叠在需求拆解层。

## 3. 需求包拆解
### 需求包 A：Session 多模态外存最小闭环
#### 目标
解决核心主路径问题：session 事件存储不再无约束保存 inline 多模态大对象，同时保证运行时可用、历史数据可读、新数据可回放。

#### 范围
本需求包包含治理契约、artifact 最小承载、session persisted view、hydrate/replay、历史兼容和必要文档测试。

覆盖路径：
- 用户侧输入：
    - 用户消息、seed history、消息改写结果。
- 协议输入：
    - AG-UI / A2A / OpenAI-compatible 等协议输入写入 session 事件存储的消息。
- 响应事件：
    - assistant / provider / remote agent response 等写入 session 事件存储的响应事件。
- 结构化多模态消息：
    - 工具或应用层派生出的结构化多模态消息。
    - 已进入 session 事件存储的工具结果消息。仅治理消息结构层面的 inline 内容；工具结果 JSON 内部的 inline blob 由需求包 C 处理。

核心能力：
- 治理契约：
    - 定义最小治理契约：inline 内容、外部引用、artifact 引用、data URL 处理原则。
    - 抽象出可复用的治理原语，避免只服务于 session 单点。
- 持久化边界：
    - 在统一持久化边界构造 persisted view。
    - 所有 session backend 复用同一治理逻辑，不在每个 DB backend 中重复实现。
- 运行时与恢复：
    > 写入轻量化，读取按需恢复。
    - runtime view 与 persisted view 分离。
    - 需要治理的 inline bytes/base64/data URL 保存为 artifact 或保留为外部引用。
    - session 事件存储中保存轻量引用和必要元信息。
    - 历史 inline session、新引用化 session、混合 session 都必须可读取和继续对话。
    - 提供 hydrate/replay 能力，至少覆盖继续对话和基础回放。

治理落点要求：
- 覆盖范围：
    - 不能只覆盖单一路径，需要覆盖 runner、协议适配层、team runtime、直接 session 写入等主要写入面。
- 推荐方向：
    - 治理逻辑应位于 session backend 之上，DB backend 只存储治理后的 payload。
    - 具体插入点和上下文传递细节放到需求设计阶段确认。

#### 不做
- 其他存储面：
    - 不治理 AG-UI track 独立 payload。
    - 不治理 telemetry/debuglog/checkpoint/evalset 的全部复制面。
- 非核心能力：
    - 不提供历史数据批量迁移工具。
    - 不做完整 artifact GC、审计、权限、加密、脱敏。
    - 不治理工具结果 JSON 内部不结构化的 base64 字段；这类归需求包 C。

#### 可发布验收
- 写入与运行时：
    - 新写入的 session 事件存储不再保存被治理类型的大块 inline bytes/base64。
    - 当前运行时模型调用不受影响。
    - 多个 session backend 行为一致。
- 兼容与恢复：
    - 历史 inline session 可读取、可继续对话。
    - 新引用化 session 可读取、可继续对话。
    - 混合 session 可读取。
    - hydrate 失败有明确错误语义，不静默丢内容。
- 默认行为：
    - 未配置 artifact service 或关闭治理时，不应静默丢内容，也不应破坏现有行为。
    - 文档说明默认行为、历史兼容和业务升级影响。

#### 需要在需求设计中确认
- 引用与元信息：
    - artifact 引用字段形式和最小元信息。
    - artifact 保存命名、版本和 hash 策略。
- 默认策略：
    - 外存触发阈值。
    - artifact service 不可用或保存失败时的默认语义。
    - 默认开关：默认外存、opt-in 还是 opt-out。
- hydrate：
    - hydrate 触发策略。倾向：模型请求构造处按需 hydrate，其余 consumer 显式请求，避免默认读取回灌 bytes 抵消存储收益。

### 需求包 B：AG-UI Track 多模态治理
#### 目标
治理 AG-UI track 作为独立存储路径的多模态 payload，避免只治理 session 事件后仍在 track 回放数据中保留大块 inline 内容。

#### 范围
- AG-UI 输入与存储：
    - AG-UI 多模态输入 payload。
    - AG-UI track custom event payload。
- 回放与兼容：
    > 前端可回放，历史可兼容。
    - MessagesSnapshot / 前端 replay。
    - 与需求包 A 的引用格式和 hydrate 能力保持一致。
    - 历史 AG-UI track 数据兼容。

#### 不做
- 不重新设计 AG-UI 协议。
- 不处理所有前端渲染策略细节。
- 不治理非 AG-UI track 的所有业务自定义 payload。

#### 依赖
- 需求包 A。

#### 可发布验收
- 新 AG-UI track 不再无约束保存大块 inline payload。
- 前端 replay 可根据引用展示、恢复或给出可解释提示。
- 历史 track 数据仍可读取。
- AG-UI 输入进入 session 事件和 track 的治理语义一致。

#### 需要在需求设计中确认
- MessagesSnapshot 返回 ref、URL、摘要还是 hydrate 后内容。
- 是否对非 AG-UI track 提供通用约束或仅文档提示。

### 需求包 C：Tool Result Inline Blob 与结果表示治理
#### 目标
治理工具结果 JSON 中现存和未来的 inline blob 风险，并约束工具输出文件如何表达为 artifact/workspace ref。

#### 范围
- 现存 inline blob：
    - 已确认会在工具结果 JSON 中内联图片、PDF 或文件内容的路径。
    - 通用工具结果 JSON 中的 inline base64/data URL/文件内容。
- 工具结果表示：
    > 先治理存储表达，不扩展模型能力。
    - 工具输出文件在结果消息中如何表达为 artifact/workspace ref。
    - 默认工具结果消息进入 session 事件存储时的治理语义。
    - 与 telemetry/debuglog/eval 复制面的交互边界。

#### 不做
- 非本包问题：
    - 不治理大文本工具结果的摘要/压缩问题。
    - 不定义完整 workspace GC。
- 兼容边界：
    - 不要求所有第三方工具立即改造输出协议。
    - 不把结构化多模态消息作为本包主范围；它们属于需求包 A 的 persisted view。
    - 不新增“tool result 直接以多模态结构给 LLM”的能力；这是未来能力项，不是当前治理主线。

#### 依赖
- 需求包 A。

#### 可发布验收
- 已知内置工具的图片/PDF inline blob 不再无约束进入长期事件存储。
- 工具 JSON 中已知 inline blob 优先在工具侧规范化为 artifact/workspace ref；通用 JSON 扫描只能作为兜底策略。
- 工具输出文件在 result message 中优先通过 artifact 或 workspace ref 表达。
- 默认工具结果消息与需求包 A 的 persisted view 不冲突。
- 相关 debug/eval 复制面至少不继续无约束复制新增 inline 大对象，或明确纳入需求包 E。

#### 需要在需求设计中确认
- 已知内置工具是在工具侧直接改输出，还是先在持久化前转换。
- 对第三方 tool result JSON 是否做通用扫描；如果做，扫描规则如何避免误伤。
- 工具结果中 ref、摘要、文件名、mime、大小如何表达。

### 需求包 D：Workspace Dereference 与临时文件治理
#### 目标
约束 conversation file 在 workspace/codeexecutor/skill 场景中的字节物化和生命周期，避免外部引用被读成 bytes 后再次进入长期状态存储。

#### 范围
- 输入物化：
    - 外部引用、provider 文件引用、host path、inline 文件数据等被物化为 workspace bytes 的路径。
- 文件产物：
    > 关注文件物化后如何引用化。
    - codeexecutor/skill 输入输出文件。
    - 应用侧上传存储与 session/artifact 引用关系。

#### 不做
- 不改变 sandbox 文件系统职责。
- 不实现完整 workspace GC。
- 不处理所有业务自定义本地文件生命周期。

#### 依赖
- 需求包 A。

#### 可发布验收
- 外部引用被物化为 workspace bytes 的路径可追踪。
- 临时文件不进入 session 等长期存储本体。
- 输出文件优先以 artifact/workspace ref 表达。
- 外部引用下载失败有可解释语义。
- 应用侧 uploads 与 session/artifact 引用关系有明确说明或约束。

#### 需要在需求设计中确认
- workspace 文件清理时机。
- workspace ref 与 artifact ref 的默认关系。
- provider file ID 下载失败时是否中断 tool/skill。

### 需求包 E：Telemetry / Debuglog / ExecutionTrace 治理
#### 目标
避免观测和调试系统复制完整多模态 payload。

#### 范围
- E1 默认止血：
    - 对观测、trace、debug snapshot 中的 inline bytes/base64/data URL 做 omit / truncate / summary，避免默认复制完整 payload。
    - E1 可独立于需求包 A 先发，用于快速止血。
- E2 引用化展示：
    - 在需求包 A 的引用原语可用后，在观测/调试中展示 ref、摘要或受控 blob。
    - E2 依赖需求包 A 的引用格式、元信息和 hydrate 能力。
- 覆盖对象：
    > 覆盖不进 session 但会被记录的内容。
    - tool args/result 中的 base64/data URL。
    - 非 session 注入消息。
    - 应用侧 debug recorder 的治理或边界声明。

#### 不做
- 不提供完整审计/合规能力。
- 不在整体规划中固定最终策略，具体需求中确认引用化、截断、omit、drop 或 debug opt-in。

#### 可发布验收
- 默认观测/调试不再无约束输出完整 inline 多模态。
- 调试需要完整内容时必须显式 opt-in 或使用受控 blob 保存能力。
- 非 session 注入消息中的 inline 多模态不会被 telemetry/debuglog 无约束复制。
- 应用侧 debug recorder 的归属、默认安全策略或排除边界有明确说明。
- 历史 debug 文件读取不被破坏。

#### 需要在需求设计中确认
- 各观测系统默认策略。
- 截断阈值和摘要格式。
- ref 是否允许在观测系统中展示。
- 应用侧 debug recorder 是并入本包治理，还是作为应用侧单独治理。

### 需求包 F：Checkpoint 与 State 泄漏守护
#### 目标
治理真实存在的 checkpoint 多模态落盘风险，并守护通用 state 不被框架内部重新引入大对象写入。

#### 范围
- checkpoint 真实风险：
    - checkpoint 中的消息和状态快照。
    - 需求包 A 完成后，历史消息引用化会自然降低 checkpoint 体积；当前轮、one-shot、agent-input 等仍需单独评估。
- 子图状态透传：
    > 防止子图状态绕过过滤。
    - 子图 completion state 透传到父 session 的路径。
    - 防止 messages 等多模态 state 绕过 graph completion 过滤。
- StateMap / StateDelta 守护：
    - 守护框架内部 state 写入不重新引入 raw bytes/base64。
    - 对业务写入 state 的大对象风险提供文档约束。

#### 不做
- 不把 StateMap 当作独立入口全量扫描治理。
- 不自动外存所有业务自定义 state。
- 不在整体规划中决定所有 checkpoint 字段恢复策略。
- 不把当前不存在的“框架内部大量写入多模态 StateMap”当作外存闭环需求。

#### 依赖
- 需求包 A。

#### 可发布验收
- checkpoint：
    - graph checkpoint 不应无约束保存完整 inline 多模态 messages。
    - checkpoint 恢复语义不被破坏。
- 子图状态透传：
    - 子图 final state relay 不应把 `messages` 等多模态 state 透传进父 session。
- StateMap / StateDelta 守护：
    - graph completion state delta 的剥离行为有回归测试。
    - 框架内部 StateMap / StateDelta 写入点有守护测试或约束，确保不写入 raw 多模态 blob。
    - 业务写入 StateMap 的大对象风险有文档约束。
- 兼容性：
    - 历史 checkpoint/state 数据兼容。

#### 需要在需求设计中确认
- 哪些 checkpoint state 必须完整可恢复。
- checkpoint 是在写入前引用化，还是依赖需求包 A 的 session history 引用化降低体积。
- 子图 relay 应复用 graph completion 过滤，还是引入更小范围的 relay 专用过滤。
- 是否为业务自定义 state 增加可选大小/key 策略，或仅做文档约束。

### 需求包 G：Evaluation / EvalSet 治理
#### 目标
避免 evaluation recorder / evalset / eval result 成为长期多模态 payload 复制面。

#### 范围
- 录制内容：
    - eval recorder 录制 user content、context messages、intermediate responses、final response。
    - evalset local/mysql。
    - eval result / benchmark output。
- 回放能力：
    > 录制减重，回放可恢复。
    - eval replay 对引用化内容的恢复。

#### 不做
- 不改变评测语义。
- 不强制决定 eval asset 必须托管到 artifact 还是业务存储。

#### 依赖
- 需求包 A。
- 如果复用观测/调试策略，则依赖需求包 E。

#### 可发布验收
- 录制多模态 case 时不默认保存完整 inline bytes。
- eval replay 能按引用恢复必要内容。
- 线上流量录制场景有明确风险提示或默认保护。
- 历史 evalset/eval result 兼容读取。

#### 需要在需求设计中确认
- eval asset 保存 ref、artifact 副本、摘要还是业务外部引用。
- 是否与 telemetry/debuglog 共用截断/摘要策略。

### 需求包 H：历史数据迁移工具
#### 目标
在运行时兼容已经满足的前提下，提供可选历史数据迁移工具，降低存量 DB 存储膨胀。

#### 范围
- 迁移动作：
    - 扫描历史 session events 中的 inline 多模态内容。
    - 将可迁移内容写入 artifact 或业务指定承载层。
    - 将历史 event 更新为引用化形态。
- 工具能力：
    > 这是存量优化，不是兼容前提。
    - 提供 dry-run、统计、失败报告和回滚建议。

#### 不做
- 不作为运行时兼容的前置条件。
- 不保证迁移所有业务自定义 JSON payload。

#### 依赖
- 需求包 A。

#### 可发布验收
- 不运行迁移工具时，历史数据仍可被新版本读取。
- dry-run 可输出影响范围和容量估算。
- 迁移失败有明确报告，不破坏原数据。

#### 需要在需求设计中确认
- 支持哪些 session backend。
- 是否支持在线迁移。
- 是否需要备份/回滚工具。

## 4. 横向要求
每个需求包都必须包含：
- 兼容性：
    - 新旧数据、混合数据、历史落盘 DB。
- 默认行为：
    - 默认是否外存。
    - 失败如何表现。
- 测试：
    - 单元测试。
    - 关键集成测试。
    - 至少覆盖一个持久化 backend。
- 文档：
    - 面向业务方的行为变化和升级说明。
- 不变量：
    - 状态/事件/观测存储不应无约束保存大对象本体。

## 5. 建议排期关系
最小可用闭环：
```text
需求包 A：Session 多模态外存最小闭环
```

需求包 A 内部可以拆实现 PR，但不能拆成多个独立发布版本。如果拆 PR，需要保证中间 PR 不改变默认行为，或通过内部开关隐藏未闭环能力。

后续可按关注度选择：
```text
需求包 B：AG-UI Track 多模态治理
需求包 C：Tool Result Inline Blob 与结果表示治理
需求包 E：Telemetry / Debuglog / ExecutionTrace 治理
需求包 D：Workspace Dereference 与临时文件治理
需求包 F：Checkpoint 与 State 泄漏守护
需求包 G：Evaluation / EvalSet 治理
需求包 H：历史数据迁移工具
```

推荐优先级：
- 核心 session 存储膨胀：
    - 先做 A。
- 团队近期重点是 AG-UI：
    - A 后优先 B。
- 担心现存内置工具已经写入 base64：
    - A 后优先 C。
- 担心观测/调试系统复制大对象：
    - 可先做 E1 默认止血；需要引用化展示时再依赖 A 做 E2。
