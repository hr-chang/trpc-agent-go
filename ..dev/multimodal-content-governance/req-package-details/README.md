# 需求包技术细节索引

## 文档定位
本目录承接 `../req-breakdown.md` 中不适合直接放在需求拆解层的代码链路、字段、函数和实现证据。

阅读关系：
- `../req-breakdown.md`：面向需求评审，描述需求包目标、范围、边界、依赖和验收。
- 本目录：面向需求设计和实现评审，记录每个需求包为什么成立，以及哪些代码路径需要重点关注。
- `../storage-path-inventory.md`：完整入口到存储路径盘点，本目录只提取和需求包直接相关的技术细节。

## 文件列表
- 需求包 A：Session 多模态外存最小闭环。
    - [`01_A_session_ex/req-scope.md`](./01_A_session_ex/req-scope.md)
    - [`01_A_session_ex/tech-design.md`](./01_A_session_ex/tech-design.md)
    - [`01_A_session_ex/core-issues.md`](./01_A_session_ex/core-issues.md)
- [`02_B_client_replay.md`](./02_B_client_replay.md)：需求包 B，AG-UI / Client Replay 多模态治理。
- [`03_C_tool_output.md`](./03_C_tool_output.md)：需求包 C，Tool Result / Execution Output 表示治理。
- [`04_D_workspace_outputs.md`](./04_D_workspace_outputs.md)：需求包 D，Workspace / Sandbox / Skill 文件产物治理。
- [`05_E1_observe_stopgap.md`](./05_E1_observe_stopgap.md)：需求包 E1，Telemetry / Debuglog / ExecutionTrace 默认止血。
- [`06_E2_observe_refs.md`](./06_E2_observe_refs.md)：需求包 E2，观测调试引用化展示与受控 Hydrate。
- [`07_F_graph_state_hitl.md`](./07_F_graph_state_hitl.md)：需求包 F，Graph Checkpoint / State / HITL Payload 泄漏守护。
- [`08_G_eval.md`](./08_G_eval.md)：需求包 G，Evaluation / EvalSet 治理。
- [`09_H_migration.md`](./09_H_migration.md)：需求包 H，历史数据迁移工具。
- [`10_I_provider_attach.md`](./10_I_provider_attach.md)：需求包 I，Provider Attachment Request Optimization。

## 通用技术约定
### 运行时视图与持久化视图
- runtime view：
    - 模型调用、工具执行、前端即时展示等运行时链路仍可使用原始 bytes、base64 或 data URL。
- persisted view：
    - 写入 session、track、checkpoint、trace、debuglog、evalset 等长期或半长期存储前，替换为轻量引用、摘要和必要元信息。
- hydrate / replay：
    - 继续对话、回放、评测、调试等需要原始内容时，通过引用按需恢复。

### 主要治理对象
- inline bytes：
    - `model.ContentPart.Image.Data`
    - `model.ContentPart.Audio.Data`
    - `model.ContentPart.File.Data`
    - 其他被 path/ref dereference 后进入内存的文件 bytes。
- base64 / data URL：
    - OpenAI-compatible `image_url.url` 中的 data URL。
    - tool result JSON 内部的 base64 字段。
    - debug/eval/trace 中序列化后的 base64。
- 外部引用：
    - 普通 URL、provider file ID、host ref、业务对象存储 URL、`artifact://` 等不默认重托管，但需要保存足够元信息支持恢复或解释失败。
