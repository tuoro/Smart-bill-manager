# Smart Bill Manager 前端

- 技术栈：Vue 3 + Vite + TypeScript + Pinia + Vue Router
- UI：PrimeVue + PrimeIcons + PrimeFlex

## 本地开发

```bash
npm ci
npm run dev
```

## 质量检查

```bash
npm run lint:ci
npm run test:run
npm run build
```

`eslint-suppressions.json` 记录重构前已有的 ESLint 错误，`lint:ci` 同时限制现有警告总数。
修复历史问题后使用以下命令清理已失效的抑制项，基线只能缩减，不能扩张：

```bash
npx eslint . --prune-suppressions
```

## 生产构建

```bash
npm run build
```
