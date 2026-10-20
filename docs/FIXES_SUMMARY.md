# 数据源页面问题修复总结（已更正）

> 说明：本文件此前记录的“加更健壮的重试逻辑”诊断是**错误的**，代码里本来就已经有 3 次重试且并未解决任何问题。
> 正确的根因与完整审计见 `AUDIT_REPORT.md`。以下为更正后的结论。

## 问题现象

安装后运行桌面应用：
1. 点击「数据源」菜单，画面始终显示 `Loading...`，不显示数据库中已初始化的数据源；
2. 点击 `+ Add Source` 按钮没有任何反应。

## 真实根因

**应用以 zoneless 模式运行，但没有意识到这一点。**

- `frontend/angular-app/package.json` 无 `zone.js` 依赖，`angular.json` 无 polyfills 配置；
- 因此 Angular 22 不会因 Promise/`setTimeout` 回调而自动触发变更检测（只有 signal、模板事件等显式通知才会）；
- `datasource.page.ts` 的 `pageReady` 在 `await this.dsService.list()` **之后**才被置为 `true`，没有任何 signal 写入 → 变更检测不再运行：
  - `@if (!pageReady)` 永远停在 loading 分支 → 一直显示 `Loading...`；
  - `[disabled]="!pageReady"` 永远为 `true` → `+ Add Source` 被禁用，看起来“点了没反应”。

后端与 Wails 绑定**完全正常**，实测证据：

```
svc-list-raw: ms=6 len=101        # DataSourceList() 6ms 返回 101 条
cd: pageReady set true            # 状态确实被置为 true
dom600ms: <button disabled="">+ Add Source</button>
          <div class="loading">Loading...</div>   # 视图仍停在首帧
```

## 修复方案

把 `DataSourcePage` 中所有“在 `await` 之后写入并被模板读取”的状态改为 Angular signal：

`pageReady` / `sources` / `showAddModal` / `editingSource` / `testResult` / `editForm` / `buttonLoading` / `saving`

模板同步由属性读取改为 `pageReady()` 调用形式，`ngModel` 改为 `[ngModel]` + `(ngModelChange)` 写 signal。
同时删除了原本无效的 `ngOnInit` 重试循环（`DataSourceService.list()` 内部已 catch 并返回 `[]`，永不抛出，重试分支是死代码）。

## 修改文件

1. `frontend/angular-app/src/app/pages/datasource/datasource.page.ts`

## 验证结果

重新 `wails build` + NSIS 打包，静默安装后实测：
- 「数据源」页面正常渲染表格，显示真实数据源（中国新闻网、Defense News 等）；
- `+ Add Source` 可点击，弹出 Add Source 表单模态框（Name / Type / URL / Request Interval / Save / Cancel）。

## 顺带修复

- `build-desktop.bat`：安装包输出路径定位、`main.go` 备份/还原、嵌入产物校验（详见 `AUDIT_REPORT.md`）；
- `backend/wails.json`：`wailsjsdir` 指向前端目录，消除手工维护的绑定副本不同步隐患。

## 遗留建议

- 其他页面（settings/recommend/followed/hotrank/各分类页）的 `loading` 同样是 `await` 后置位，**loading 提示不会显示**。根治方式二选一：引入 `zone.js`（改动最小，参考项目即如此），或把异步状态统一改 signal。
- 建议改用 Wails 原生 `wails build -nsis`，可自动捆绑 WebView2 运行时并正确注册卸载信息。
- 数据库 `sources` 表存在 104 条历史空白记录，建议备份后清理。
