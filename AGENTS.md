# 项目协作约定

## 项目结构

- [需求分析](docs/requirements.md)记录配置范围、客户端兼容性和验收场景。
- [技术设计](docs/technical-design.md)记录 Vite、Go、SQLite 的架构和接口方案。
- `prototype/` 存放交互原型。修改原型后，在该目录运行 `npm run build`，并验证受影响的交互。
- `frontend/` 存放正式前端。修改后运行 `npm --prefix frontend test` 和 `npm --prefix frontend run build`，并验证受影响的交互。
- `cmd/clash-studio/` 和 `internal/` 存放 Go 入口和后端模块。修改后在项目根目录运行 `go test ./...`。

## Git 提交

- 每次完成一项有文件变更的改动或迭代，验证后使用 Git 提交一次。
- 提交前检查 `git status` 和暂存区差异，只暂存本次任务的相关改动。提交信息应说明具体变更。
