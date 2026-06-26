# 多模态内容治理文档入口

## 文档结构
- 总览规划：
    - [`overview.md`](./overview.md)
    - 说明整体背景、目标、收益、治理原则、边界和已确认决策。
- 向上汇报 / 讨论稿：
    - [`discussion-brief.md`](./discussion-brief.md)
    - 从总览中抽取更浓缩的表达，适合高层汇报或会议讨论。
- 入口与存储路径盘点：
    - [`storage-path-inventory.md`](./storage-path-inventory.md)
    - 从第一手多模态入口出发，逐条追踪到运行时消费、存储、外发、回放和治理判断。
- 需求包拆解：
    - [`requirement-packages.md`](./requirement-packages.md)
    - 将整体规划拆成可发布的需求包，只保留需求层面的目标、范围、边界、依赖和验收口径。
- 需求包技术细节：
    - [`requirement-package-details/`](./requirement-package-details/)
    - 按需求包拆分，承接代码链路、字段、函数、现存风险点和实现证据。

## 推荐阅读顺序
- 快速了解背景和结论：
    - 先读 `discussion-brief.md`。
- 评审整体规划：
    - 读 `overview.md`。
    - 必要时查 `storage-path-inventory.md`。
- 评审需求拆分：
    - 先读 `requirement-packages.md`。
    - 对具体链路或代码事实有疑问时，再读 `requirement-package-details/` 下对应需求包文档。

## 分层原则
- `overview.md` 负责回答“为什么做、整体边界是什么”。
- `storage-path-inventory.md` 负责回答“有没有盘全、内容从哪里来到哪里去”。
- `requirement-packages.md` 负责回答“拆成哪些可发布需求包”。
- `requirement-package-details/` 负责回答“这些需求包对应哪些代码事实和实现关注点”。
