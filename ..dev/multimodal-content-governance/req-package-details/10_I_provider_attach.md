# 需求包 I 技术细节：Provider Attachment Request Optimization

## 技术动机
- A 包保证 session persisted view 不保存大对象本体。
- 模型请求前仍需要把 internal ref 转换成 provider 可消费的内容。
- 对支持附件/file API 的 provider，可以把 artifact/workspace 内容上传为 provider file，再在主请求中使用 provider file id/ref。

## 一次性附件收益
- 即使附件只用一次，也可能有收益：
    - 主模型请求不携带大块 bytes/base64。
    - 减少 base64/JSON 体积放大。
    - 多附件可以并发上传。
    - 上传可与 prompt/context 构造并行。
- 如果只能在模型请求前同步串行上传一次，端到端延迟收益有限，主要收益是主请求减重和稳定性。

## 重点链路
- 输入来源：
    - A 包 `ContentRef`。
    - C 包 tool result / execution output ref。
    - D 包 workspace/artifact ref。
- provider 表达：
    - provider file id。
    - provider attachment ref。
    - file data。
    - file URL / image URL。
- fallback：
    - provider 不支持上传时，回退到 hydrate 后的既有表达。
    - file id 失效时，重新 hydrate + upload 或返回明确错误。

## 设计注意点
- provider file id 通常不能跨 provider / model 复用。
- cache 作用域需要保守：
    - 当前请求内 cache。
    - 短生命周期 invocation cache。
    - 是否持久化到 session/artifact metadata 需单独评估。
- 不应把框架内部 `artifact://` 直接发给 provider。
- 上传失败必须有明确错误或 fallback 语义。
- 这不是 A 的正确性闭环，而是 provider request 性能和请求形态优化。
