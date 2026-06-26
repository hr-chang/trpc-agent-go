# 需求包 E 技术细节：Telemetry / Debuglog / ExecutionTrace 治理

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

## 设计注意点
- E1 默认止血可以不依赖需求包 A：
    - omit。
    - truncate。
    - summary。
    - drop full payload unless debug opt-in。
- E2 引用化展示依赖需求包 A：
    - 展示 artifact ref。
    - 展示摘要和元信息。
    - 需要完整内容时走受控 hydrate 或 blob 查看能力。
- debug 模式保存完整内容应显式 opt-in，不能成为默认行为。
