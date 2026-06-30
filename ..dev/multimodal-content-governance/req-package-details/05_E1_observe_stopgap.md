# 需求包 E1 技术细节：Telemetry / Debuglog / ExecutionTrace 默认止血

## 重点复制面
- telemetry / OTLP。
- Langfuse。
- debuglog。
- ExecutionTrace。
- OpenClaw debug recorder。

## 易被复制的内容
- model request messages。
- provider response。
- tool args/result。
- `InjectedContextMessages` / `LateContextMessages`。
- 由 `json.Marshal` 得到的 messages/responses snapshot。
- workspace / skill / codeexecutor 输出结果。

## 默认止血策略
- E1 不等待完整引用化展示，可先独立发布：
    - omit。
    - truncate。
    - summary。
    - drop full payload unless debug opt-in。
- 默认策略应满足：
    - 不无约束输出完整 bytes/base64/data URL。
    - 不破坏历史 debug 文件读取。
    - 保留足够排查信息，例如 mime、大小、hash、字段路径、截断原因。

## 设计注意点
- debug 模式保存完整内容应显式 opt-in，不能成为默认行为。
- tool result / execution output 的新增引用形态，应避免被观测面再次展开为完整内容。
- E1 的目标是止血，不要求 trace/debug viewer 能直接打开完整 blob。
