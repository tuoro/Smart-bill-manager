# Smart Bill Manager 前端

前端使用 Vue 3、TypeScript、Vite、Pinia、Vue Router 和 PrimeVue。模块职责与状态流转见 [架构说明](../docs/architecture.md#前端分层)。

## 本地开发

```bash
npm ci
npm run dev
```

Vite 默认监听 <http://localhost:5173>，并将 `/api` 代理到 <http://localhost:3001>。

可选环境变量：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `VITE_API_URL` | `/api` | API 根地址 |
| `VITE_FILE_URL` | 空 | 文件地址前缀 |
| `VITE_API_TIMEOUT_MS` | `15000` | API 超时毫秒数 |
| `VITE_API_CONCURRENCY` | `6` | 最大并发请求数 |

## 目录约定

- `src/api/client.ts`：唯一 Axios 实例和请求级基础设施。
- `src/api/storage.ts`：认证及管理员代操作的本地持久化。
- `src/api/*.ts`：按业务域组织的接口封装。
- `src/stores`：跨页面共享的运行时状态。
- `src/composables`：可复用异步流程，不包含页面展示。
- `src/components`：具有明确 props 和 emits 的领域 UI。
- `src/views`：路由页面，只负责编排领域能力。

新增接口不得在页面内直接调用 Axios；新增轮询或分页流程应优先复用现有 composable。

## 质量检查

```bash
npm run lint:ci
npm run test:run
npm run build
```

`lint:ci` 要求零警告。`eslint-suppressions.json` 已清空，不得重新引入历史类型或规则抑制；确认删除旧抑制后可运行：

```bash
npx eslint . --prune-suppressions
```

共享 API、store、composable 和工具层的行为变化必须补充 Vitest 测试。
