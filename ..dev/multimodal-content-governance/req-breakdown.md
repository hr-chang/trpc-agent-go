# 多模态内容治理需求拆解

## 1. 文档定位
本文基于 `overview.md` 和 `storage-path-inventory.md`，将多模态内容治理规划拆成可讨论、可排期、可发布的需求包。

本文只描述需求层面的目标、范围、边界、依赖和验收口径。涉及具体代码链路、字段、函数、现存风险点和实现证据的内容，统一放在 `req-package-details/` 下的 per-package 技术细节文档。

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

### 需求包 B：AG-UI / Client Replay 多模态治理
#### 目标
治理客户端回放面中的多模态 payload，避免只治理 session 事件后，AG-UI track、MessagesSnapshot、event bridge 或 SSE cache 等回放路径仍保存大块 inline 内容。

#### 范围
- AG-UI 输入与存储：
    - AG-UI 多模态输入 payload。
    - AG-UI track custom event payload。
- 客户端回放面：
    > 有存储或回放职责的客户端面纳入治理。
    - MessagesSnapshot / 前端 replay。
    - event bridge 或 SSE translator 中带持久化、缓存、回放语义的 payload。
    - 与需求包 A 的引用格式和 hydrate 能力保持一致。
    - 历史 AG-UI track 数据兼容。

#### 不做
- 不重新设计 AG-UI 协议。
- 不处理所有前端渲染策略细节。
- 不治理纯透传、无存储、无回放语义的业务 SSE translator。
- 不治理非 AG-UI / 非 client replay 的所有业务自定义 payload。

#### 依赖
- 需求包 A。

#### 可发布验收
- 新 AG-UI track 不再无约束保存大块 inline payload。
- 前端 replay 可根据引用展示、恢复或给出可解释提示。
- 历史 track 数据仍可读取。
- AG-UI 输入进入 session 事件和 track 的治理语义一致。
- 有存储/回放职责的 client replay 面有明确治理或边界声明。

#### 需要在需求设计中确认
- MessagesSnapshot 返回 ref、URL、摘要还是 hydrate 后内容。
- 哪些 event bridge / SSE 形态属于框架治理面，哪些只是业务透传边界。
- 是否对非 AG-UI track 提供通用约束或仅文档提示。

### 需求包 C：Tool Result / Execution Output 表示治理
#### 目标
治理工具结果和执行产物中的 inline blob 风险，约束工具、Skill、CodeExecutor、workspace、subagent 和异步工具结果如何表达为 artifact/workspace/task ref。

#### 范围
- inline blob：
    - 已确认会在工具结果 JSON 中内联图片、PDF 或文件内容的路径。
    - 通用工具结果 JSON 中的 inline base64/data URL/文件内容。
- 执行产物表示：
    > 先治理结果表达，避免大对象塞回消息。
    - Skill / CodeExecutor / workspace 输出文件在结果消息中如何表达为 artifact/workspace ref。
    - subagent 返回文件或多模态结果时的引用与摘要表达。
    - 异步工具返回 `task_id`、状态 ref、结果 ref，而不是直接返回大 payload。
    - 默认工具结果消息进入 session 事件存储时的治理语义。
    - 与 telemetry/debuglog/eval 复制面的交互边界。

#### 不做
- 不治理普通大文本工具结果的完整摘要/压缩体系；必要的截断或 ref 表达只服务于大对象治理。
- 不定义完整 workspace GC。
- 不要求所有第三方工具立即改造输出协议。
- 不把结构化多模态消息作为本包主范围；它们属于需求包 A 的 persisted view。
- 不新增“tool result 直接以多模态结构给 LLM”的能力；这是未来能力项，不是当前治理主线。
- 不设计完整异步工具框架，只定义大对象结果的安全表达边界。

#### 依赖
- 需求包 A。

#### 可发布验收
- 已知内置工具的图片/PDF inline blob 不再无约束进入长期事件存储。
- 工具 JSON 中已知 inline blob 优先在工具侧规范化为 artifact/workspace ref；通用 JSON 扫描只能作为兜底策略。
- 工具、Skill、CodeExecutor、workspace 输出文件在 result message 中优先通过 artifact/workspace/task ref 表达。
- 默认工具结果消息与需求包 A 的 persisted view 不冲突。
- 相关 debug/eval 复制面至少不继续无约束复制新增 inline 大对象，或明确纳入需求包 E1/G。

#### 需要在需求设计中确认
- 已知内置工具是在工具侧直接改输出，还是先在持久化前转换。
- 对第三方 tool result JSON 是否做通用扫描；如果做，扫描规则如何避免误伤。
- 工具结果中 ref、摘要、文件名、mime、大小如何表达。
- 异步工具的 `task_id` / result ref 是否只作为表达约束，还是纳入更完整的异步工具能力。

### 需求包 D：Workspace / Sandbox / Skill 文件产物治理
#### 目标
治理 workspace、sandbox、remote CodeExecutor、Skill 执行中的文件物化、执行产物表达和临时文件生命周期，避免文件被读成 bytes 后再次进入长期状态存储。

#### 范围
- 输入物化：
    - 外部引用、provider 文件引用、host path、inline 文件数据等被物化为 workspace bytes 的路径。
- 文件产物：
    > 关注执行文件在哪里、如何引用、何时清理。
    - local / remote CodeExecutor 输入输出文件。
    - per-session workspace 文件。
    - Skill 输入输出文件。
    - sandbox 中间文件和产物目录。
    - workspace ref 与 artifact ref 的关系。
    - 应用侧上传存储与 session/artifact 引用关系。

#### 不做
- 不改变 sandbox 文件系统职责。
- 不实现完整 workspace GC。
- 不处理所有业务自定义本地文件生命周期。
- 不替代需求包 C 的 tool result 表达治理。

#### 依赖
- 需求包 A。
- 与需求包 C 强关联。

#### 可发布验收
- 外部引用被物化为 workspace bytes 的路径可追踪。
- 临时文件不进入 session 等长期存储本体。
- 输出文件优先以 artifact/workspace ref 表达。
- 外部引用下载失败有可解释语义。
- workspace / sandbox / Skill 产物与 session/artifact 引用关系有明确说明或约束。

#### 需要在需求设计中确认
- workspace 文件清理时机。
- workspace ref 与 artifact ref 的默认关系。
- provider file ID 下载失败时是否中断 tool/skill。
- remote CodeExecutor 与本地 workspace 的生命周期边界。

### 需求包 E1：Telemetry / Debuglog / ExecutionTrace 默认止血
#### 目标
先避免观测和调试系统默认复制完整多模态 payload，作为可独立发布的止血能力。

#### 范围
- 默认策略：
    > 先止住默认复制，不等待引用化展示。
    - 对观测、trace、debug snapshot 中的 inline bytes/base64/data URL 做 omit / truncate / summary。
    - 对 tool args/result、model request/response、非 session 注入消息中的大对象执行统一保护。
- 覆盖对象：
    - telemetry / OTLP。
    - Langfuse。
    - debuglog。
    - ExecutionTrace。
    - 应用侧 debug recorder 的治理或边界声明。

#### 不做
- 不提供完整审计/合规能力。
- 不提供完整引用化展示。
- 不承诺调试系统能直接查看完整 blob；完整内容查看归 E2 或业务 opt-in。

#### 依赖
- 可独立于需求包 A 发布。

#### 可发布验收
- 默认观测/调试不再无约束输出完整 inline 多模态。
- 调试需要完整内容时必须显式 opt-in 或使用受控 blob 保存能力。
- 非 session 注入消息中的 inline 多模态不会被 telemetry/debuglog 无约束复制。
- 应用侧 debug recorder 的归属、默认安全策略或排除边界有明确说明。
- 历史 debug 文件读取不被破坏。

#### 需要在需求设计中确认
- 各观测系统默认策略。
- 截断阈值和摘要格式。
- debug opt-in 的开关位置和安全提示。

### 需求包 E2：观测调试引用化展示与受控 Hydrate
#### 目标
在引用原语稳定后，让观测、debug、trace 能展示 ref、摘要和必要元信息，并在受控场景下按需 hydrate 完整内容。

#### 范围
- 引用化展示：
    - artifact/workspace/provider ref。
    - mime、大小、hash、文件名等摘要元信息。
    - 与 session event、tool result、workspace output 的关联信息。
- 受控 hydrate：
    > 需要完整内容时才显式恢复。
    - debug UI / trace viewer / replay 工具按需恢复。
    - hydrate 失败给出可解释错误。
    - 访问控制、权限、审计只保留接口空间，不在本包完整实现。

#### 不做
- 不替代 E1 的默认止血。
- 不提供完整合规审计系统。
- 不保证所有第三方观测 backend 都支持 blob 查看。

#### 依赖
- 需求包 A。
- 通常依赖需求包 C/D 的结果引用形态稳定。
- 建议在 E1 后实施。

#### 可发布验收
- 观测/调试中可以展示轻量 ref 和摘要。
- 显式请求完整内容时走受控 hydrate。
- hydrate 失败不会静默丢内容或显示错误内容。
- 默认路径仍不复制完整 payload。

#### 需要在需求设计中确认
- ref 是否允许在观测系统中展示。
- 受控 hydrate 的权限、开关和审计边界。
- 哪些 debug recorder 并入本包，哪些作为应用侧单独治理。

### 需求包 F：Graph Checkpoint / State / HITL Payload 泄漏守护
#### 目标
治理 Graph、Checkpoint、State、Interrupt/Resume 中真实存在的多模态落盘风险，并守护通用 state 不被框架内部重新引入大对象写入。

#### 范围
- checkpoint 真实风险：
    - checkpoint 中的消息和状态快照。
    - 需求包 A 完成后，历史消息引用化会自然降低 checkpoint 体积；当前轮、one-shot、agent-input 等仍需单独评估。
- Graph / HITL payload：
    > 关注暂停恢复时会被保存的内容。
    - Interrupt / Resume payload。
    - graph loop / dynamic dispatch state。
    - 多路并行 join 前后的中间结果。
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
- HITL / 子图状态透传：
    - interrupt/resume payload 不应无约束保存大对象本体。
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
- interrupt/resume payload 的默认保存策略。
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
- 如果复用观测/调试策略，则依赖需求包 E1。
- 若需要引用化展示或受控 hydrate，则依赖 E2。

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
- 建议等待 A/C/D 的引用形态稳定后再实施。

#### 可发布验收
- 不运行迁移工具时，历史数据仍可被新版本读取。
- dry-run 可输出影响范围和容量估算。
- 迁移失败有明确报告，不破坏原数据。

#### 需要在需求设计中确认
- 支持哪些 session backend。
- 是否支持在线迁移。
- 是否需要备份/回滚工具。

### 需求包 I：Provider Attachment Request Optimization
#### 目标
优化 provider 附件请求形态：在不影响 A 包正确性闭环的前提下，将 artifact/content ref 转换为 provider 原生 file upload / file id / attachment ref，降低主模型请求体积和 base64/JSON 放大。

#### 范围
- 一次性附件优化：
    > 不依赖复用，也可以减轻主请求。
    - hydrate 后异步上传到 provider。
    - provider 内容组装只携带 file id / provider ref。
    - 多附件并发上传。
    - 上传与 prompt/context 构造并行。
- 短期缓存：
    - artifact ref 到 provider file id 的当前请求级或短生命周期缓存。
    - provider file id 失效后的 fallback。
- provider 能力适配：
    - 按 provider/model 能力选择 file id、file data、file URL、image URL 等表达。

#### 不做
- 不改变 A 的 persisted view 语义。
- 不要求所有 provider 支持 file upload。
- 不做跨 provider 复用 provider file id。
- 不承诺长期 provider file id 生命周期管理。

#### 依赖
- 需求包 A。
- 对工具执行产物场景，通常依赖 C/D 的 ref 形态。

#### 可发布验收
- 支持附件 provider 时，主模型请求可以只携带 provider file id/ref。
- 不支持附件 provider 时，仍可回退到 hydrate 后的既有内容表达。
- 上传失败、file id 失效有明确错误或 fallback 语义。
- 不把框架内部 `artifact://` 直接外发给 provider。

#### 需要在需求设计中确认
- 支持哪些 provider 和 model。
- 上传何时启动，能否与 request 其他构造步骤并行。
- provider file id cache 的作用域、TTL 和失效处理。

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
    - 至少覆盖一个持久化 backend，或明确说明该包不涉及 session backend。
- 文档：
    - 面向业务方的行为变化和升级说明。
- 不变量：
    - 状态、事件、观测、回放、评测类存储不应无约束保存大对象本体。

## 5. 建议排期关系
已完成基线：
```text
需求包 A：Session 多模态外存最小闭环
```

A 完成后，后续优先级建议：
```text
需求包 C：Tool Result / Execution Output 表示治理
需求包 E1：Telemetry / Debuglog / ExecutionTrace 默认止血
需求包 F：Graph Checkpoint / State / HITL Payload 泄漏守护
需求包 D：Workspace / Sandbox / Skill 文件产物治理
需求包 B：AG-UI / Client Replay 多模态治理
需求包 E2：观测调试引用化展示与受控 Hydrate
需求包 G：Evaluation / EvalSet 治理
需求包 H：历史数据迁移工具
需求包 I：Provider Attachment Request Optimization
```

推荐说明：
- A 后优先 C：
    - 补齐工具、Skill、CodeExecutor、workspace、subagent 执行结果中的 inline blob 风险。
- E1 可与 C 并行：
    - 先避免观测和调试系统继续复制完整多模态 payload。
- F 上调优先级：
    - 生产场景中 Graph / HITL / checkpoint 使用频率高，状态快照风险更靠前。
- D 与 C 强关联：
    - C 解决结果表达，D 解决文件产物和临时文件生命周期。
- B 视团队近期 AG-UI / client replay 重点决定是否提前。
- E2/G/H/I 建议在引用形态和默认止血能力稳定后推进。
