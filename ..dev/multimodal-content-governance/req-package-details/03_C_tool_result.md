# 需求包 C 技术细节：Tool Result Inline Blob 与结果表示治理

## 已确认现存风险
- ClaudeCode `Read` 工具读取图片/PDF 时，会在 tool result JSON 中内联 base64。
- 风险字段：
    - `readOutput.File.Base64`
- 传播路径：
    - tool result JSON
    - 默认 tool message
    - `session.Events`
    - telemetry / debuglog / eval recorder

## 当前 tool result 表示限制
- 当前默认工具结果主要以字符串或 JSON 形式进入 tool message。
- 现阶段不应把本需求误解为新增“`RoleTool.ContentParts` 直接作为 tool result 给 LLM”的能力。
- MCP image result、OpenClaw `MEDIA:` / `MEDIA_DIR:` 派生出的 `ContentParts` 属于结构化消息治理，主路径归需求包 A。

## 设计注意点
- 已知内置工具优先在工具侧改造输出，减少后续通用扫描压力。
- 第三方工具结果 JSON 如果做通用扫描，需要避免：
    - 误判普通长字符串。
    - 破坏业务自定义 JSON schema。
    - 在大 JSON 中引入明显性能开销。
- 工具输出文件建议表达为：
    - artifact ref。
    - workspace ref。
    - 文件名、mime、大小、hash 等摘要元信息。
