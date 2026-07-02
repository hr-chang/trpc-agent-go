# 需求包 J-2 技术细节：Artifact 生命周期治理

## 技术动机
J-1 提供 artifact TTL / retention 的基础表达、过期扫描和 ArtifactService 范围内的 cleanup 删除能力，但不能判断 artifact 是否仍被业务引用。

J-2 负责把 TTL / retention 与 session/content/tool/workspace/eval/debug 等引用关系结合起来，决定哪些 artifact 可以进行治理级安全清理。

## 范围
- session 删除：
    - `DeleteSession` / 批量删除 / 用户数据清理。
- 引用扫描：
    - session event `ContentRef`。
    - tool output ref。
    - workspace output ref。
    - eval/debug/replay 保存的 ref。
- orphan 判断：
    - artifact 已无任何已知 ref 引用。
    - artifact ref 仍存在但 session 已删除或不可达。
- 安全清理：
    - expired + orphan 可清理。
    - expired 但仍被引用时不默认删除，输出报告或进入待处理状态。
    - pinned / no-expire artifact 不参与默认清理。
- 清理失败：
    - 重试。
    - 告警。
    - 人工修复入口。

## 不做
- 不实现 TTL / retention 基础 API；该能力归 J-1。
- 不替代需求包 H 的历史 session 内容迁移。
- 不管理 provider file id 的长期生命周期；provider file id 归需求包 I。
- 不定义完整 workspace GC；workspace 文件生命周期归需求包 D。
- 不做完整权限、审计、加密、脱敏。

## 设计注意点
- 第一版可优先做安全 dry-run：
    - 扫描 session refs。
    - 扫描 artifact refs。
    - 输出 orphan candidate 和仍被引用的 artifact。
- 删除策略要保守：
    - 默认不误删 pinned refs。
    - 删除失败不能破坏 session 读取。
    - 支持重试和人工确认。
- 反引用索引是否必要取决于 artifact backend 能力和规模；可先设计接口空间，首版用扫描实现。
- provider file id 生命周期不应混入本包，provider cache 属于需求包 I。

## 测试关注点
- TTL 到期但仍被 session ref 引用时不会误删。
- expired + orphan 可被 dry-run 识别。
- 同一 artifact 被多个 ref 使用时不会误删。
- 删除 session 后可列出或清理仅属于该 session 的 artifacts。
- orphan dry-run 输出稳定、可解释。
- 清理失败保留原始 session/ref 语义，并能报告失败对象。
