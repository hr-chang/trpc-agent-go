# 需求包 E2 技术细节：观测调试引用化展示与受控 Hydrate

## 依赖前提
- 需求包 A 提供 session event 中的 `ContentRef` 与 hydrate 能力。
- 需求包 C/D 稳定工具结果和 workspace 产物的 ref 表达。
- 需求包 E1 已经保证默认路径不复制完整 payload。

## 引用化展示内容
- artifact ref / workspace ref / provider file ref。
- mime type。
- size bytes。
- sha256 或其他摘要。
- 原始文件名或展示名。
- 来源字段路径，例如 message part、tool result 字段、debug snapshot 字段。
- 关联信息，例如 session、event、tool call、workspace path。

## 受控 hydrate 场景
- debug UI 查看完整内容。
- trace viewer 按需展开 blob。
- replay 工具恢复必要内容。
- eval 排查失败案例时读取完整资产。

## 设计注意点
- hydrate 必须显式触发，不能变成默认 trace/log 展开。
- hydrate 失败需要显示可解释错误，不能展示空内容冒充成功。
- 权限、审计、加密、脱敏暂不完整实现，但接口设计不能阻碍后续扩展。
- 不同观测 backend 对 blob 或链接展示能力不同，需要允许降级到 ref + metadata。
