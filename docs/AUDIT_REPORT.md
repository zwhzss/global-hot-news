# 桌面构建审计 + 数据源页面问题修复报告

日期：2026-09-28
审计对象：`build-desktop.bat`、前后端构建链、Wails/WebView2/NSIS 打包，以及“数据源页面一直 Loading / Add Source 无反应”缺陷。

---

## 一、结论速览

| # | 问题 | 根因 | 状态 |
|---|------|------|------|
| 1 | 数据源页面永远显示 `Loading...` | 应用实际以 **zoneless（无 zone.js）** 方式运行；`pageReady` 在 `await` 之后被赋值，Angular 22 不会因此调度变更检测，视图永久停留在首帧 | **已修复并验证** |
| 2 | `+ Add Source` 点击无反应 | 同一根因：按钮绑定 `[disabled]="!pageReady"`，`pageReady` 停留在 `false`，按钮被禁用 | **已修复并验证** |
| 3 | 构建产物安装包报错“Installer not found” | `GlobalHotNews.nsi` 的 `OutFile` 指向 `backend\build\`，脚本却在 `backend\build\nsis\` 查找 | **已修复** |
| 4 | `wails build` 缺少绑定生成 / 前端绑定副本不同步 | `wailsjsdir` 指向 `./wailsjs`，生成到 `backend/wailsjs/wailsjs/`，前端真正引用的 `frontend/angular-app/wailsjs` 是手工副本 | **已修复** |
| 5 | 未捆绑 WebView2 运行时 | 手写 NSIS 脚本只复制 exe，目标机若无 WebView2 则无法启动 | 建议改用 `wails build -nsis`（已验证可用，见 §4） |
| 6 | 数据库中存在 104 条空白数据源记录 | 历史遗留脏数据（空 name/url，id 20~133），非本次代码缺陷 | 建议清理（见 §5） |

---

## 二、数据源页面缺陷：定位过程与证据

### 2.1 先排除的“常见嫌疑”

逐项实测排除了以下可能：

- **Wails 绑定未注入** —— 否。从运行中的应用内取到运行时状态：`window.go.bindings.Bindings.DataSourceList` 为 `function`，`/wails/runtime.js` 响应 200，且其内容以 `window.wailsbindings='{"bindings":{"Bindings":{...` 开头，绑定映射正常。
- **后端接口报错或超时** —— 否。在运行中的应用里给 `DataSourceService.list()` 加埋点，实测 `DataSourceList()` **6ms 返回 101 条数据**，`toSource` 映射全部成功。
- **组件 `ngOnInit` 未执行** —— 否。埋点显示 `ngOnInit` 确实进入，`loadSources()` 也走到 `finally` 并把 `pageReady` 置为 `true`。
- **前端产物陈旧 / 缺文件** —— 否。`main-*.js`、`styles-*.css`、`favicon.ico`、`polyfills`（如启用）资源全部 200。

### 2.2 关键证据

在打包后的 exe 内注入埋点后，得到以下输出：

```
BROWSER t=svc-list-raw:  ms=6 len=101          <- 后端接口正常，返回 101 条
BROWSER t=svc-list-mapped: n=101               <- 前端映射正常
BROWSER t=cd: pageReady set true               <- 状态确实被置为 true
BROWSER t=dom600ms: <button ... disabled="">+ Add Source</button>
                    <div class="loading">Loading...</div>   <- 视图仍是首帧
```

再做反向验证——检测**变更检测本身**是否运行：

```
BROWSER t=domtest: loadingDiv=Loading...
BROWSER t=domtest: set to CD-PROBE             <- 手工改 DOM
BROWSER t=state-4s: <div class="loading">CD-PROBE</div>   <- 4 秒后 Angular 未覆盖
```

若变更检测正常，`pageReady=true` 后 Angular 必然会重新渲染该视图并覆盖 `CD-PROBE`。
**推断成立：状态已更新，但变更检测从未再次运行。**

### 2.3 根因

`frontend/angular-app/package.json` 没有 `zone.js` 依赖，`angular.json` 也没有 polyfills 配置，`main.ts` 只传了 `ngZoneEventCoalescing`（该选项在无 zone.js 时不生效）。因此应用处于 **zoneless 模式**：变更检测只由 signal、模板事件、`markForRefresh` 等显式通知驱动，**不会**因 Promise/`setTimeout` 回调而自动触发。

`datasource.page.ts` 原实现：

```ts
pageReady = false;
async ngOnInit() { ... await this.loadSources(); ... }
async loadSources() { try { this.sources = await this.dsService.list(); } finally { this.pageReady = true; } }
```

`pageReady = true` 发生在 `await` 之后（微任务里），没有任何 signal 写入，于是：
- `@if (!pageReady)` 永远走 loading 分支；
- `[disabled]="!pageReady"` 永远为 `true`，按钮被禁用 → “点击没反应”。

对照参考项目 `study-phy-middle-offline`：它显式依赖 `zone.js` 并在 `app.config.ts` 使用 `provideZoneChangeDetection({eventCoalescing:true})`，所以不会出现该问题。这也印证了本项目是**缺少 zone.js 而误以为仍在用 zone**。

### 2.4 修复

把 `DataSourcePage` 中所有会在 `await` 之后被写入、且被模板读取的状态改为 **signal**：

```ts
readonly sources      = signal<Source[]>([]);
readonly pageReady    = signal(false);
readonly showAddModal  = signal(false);
readonly editingSource = signal<Source | null>(null);
readonly testResult    = signal<TestResult | null>(null);
readonly editForm      = signal<EditForm>(emptyForm());
readonly buttonLoading = signal('');
readonly saving        = signal(false);
```

模板同步改为调用形式（`pageReady()`、`showAddModal()`…），`ngModel` 改为 `[ngModel]` + `(ngModelChange)` 写 signal，并顺带修正了原模板中 `[disabled]="buttonLoading === 'toggle-'+src.id"` 的写法统一为 `buttonLoading() === ...`。

另外清理了 `ngOnInit` 中实际无效的 3 次重试循环（`DataSourceService.list()` 内部已 catch 且返回 `[]`，永远不会抛出，重试分支是死代码），并让 `toggleSource` 成功后也刷新列表。

**修改文件：** `frontend/angular-app/src/app/pages/datasource/datasource.page.ts`（仅此一个前端文件）

### 2.5 验证（真实安装包）

重新走完整 `wails build` + NSIS，静默安装后启动实测：

- 点击「数据源」→ 表格正常渲染，显示真实数据源（中国新闻网、Defense News 等），含 Name/Type/URL/Status/Interval/Last Fetch 与 Disable/Test/Fetch/Delete 按钮；
- `+ Add Source` 不再禁用，点击后弹出 `Add Source` 模态框（Name / Type / URL / Request Interval / Cancel / Save）。

---

## 三、build-desktop.bat 审计结果

### 3.1 正确的部分

- 项目根、后端目录、前端目录、embed 目录、NSIS 目录的路径推导正确；
- `backend/frontend_dist` 与 `cmd/app/main.go` 里的 `//go:embed all:frontend_dist` 路径一致，前端确实被嵌进 exe（已验证新 exe 内含 `main-NYUEZ7VI.js`）；
- **“把 `cmd/app/main.go` 复制到后端根目录”这一步是必需的，不是冗余**。已实测：不复制时 `wails build` 直接失败，报 `no Go files in ...\backend`（Wails CLI 的绑定生成阶段要求模块根存在 Go 文件）。脚本原逻辑正确，只是失败路径下会无条件删除 `main.go`，可能误删用户已有文件；
- `wails build -platform windows/amd64 -ldflags "-s -w"` 参数正确（产物 14.9 MB，`windowsgui` 无控制台）；
- 每个失败分支都有 `if %errorlevel% neq 0` 检查。

### 3.2 发现的问题

| 级别 | 问题 |
|------|------|
| 中 | **安装包定位错误**：`GlobalHotNews.nsi` 的 `OutFile "..\..\build\GlobalHotNews_Setup.exe"` 实际输出到 `backend\build\`，但脚本第 166 行检查的是 `%NSIS_DIR%\GlobalHotNews_Setup.exe`，导致构建成功却打印不出安装包信息 |
| 中 | **WebView2 未捆绑**：手写 NSIS 只 `File` 复制了 exe，目标机缺 WebView2 Runtime 时应用无法启动 |
| 中 | **卸载逻辑有缺陷**：`Section "Uninstall"` 中 `MessageBox ... IDYES CleanUp IDNO NoCleanUp` 的标签走向写反（`CleanUp:` 落空、`NoCleanUp:` 反而执行 `RmDir /r /REBOOTOK "$APPDATA\GlobalHotNews"`）；且提权卸载时 `$APPDATA` 指向管理员账户而非当前用户 |
| 中 | **绑定生成位置误导**：`wailsjsdir: "./wailsjs"` 使绑定生成到 `backend/wailsjs/wailsjs/`，前端真正 import 的 `frontend/angular-app/wailsjs/` 是手工副本，容易长期不同步（本次核对时两者恰好一致，但无任何机制保证） |
| 低 | Step 1 先 `npm run build`，Step 3 的 `wails build` 又会按 `frontend:build` 再构建一次并复制一次前端，重复耗时 |
| 低 | Step 2 的 `xcopy` 源路径硬编码 Angular 项目名 `global-hot-news`，且 `angular.json` 未显式设置 `outputPath`（依赖 CLI 默认值） |
| 低 | `GONOSUMCHECK` 不是 Go 认的环境变量（应为 `GOFLAGS`/`GONOSUMDB`/`GOSUMDB`），无效设置 |
| 低 | 脚本失败退出后 `main.go` 副本可能残留（中断时）；本次已发现并修掉 |

### 3.3 已实施的修复

**`build-desktop.bat`**
- 修正安装包定位：先查 `backend\build\GlobalHotNews_Setup.exe`，回退到 `nsis\` 目录，并给出 WARNING 而不是静默无输出；
- `main.go` 改为“先备份再复制、构建结束（含失败路径）统一还原”的 `:RestoreStagedMain` 子过程，不再无条件删除；
- 复制 `main.go` 后增加存在性校验，失败即明确报错；
- Step 2 增加 `index.html` 存在性校验，避免 Angular `outputPath` 变动时悄无声息地嵌入空目录；
- 路径/总结分支改用 label（`goto`）实现——实测把 `for` 嵌在 `if (...)` 里会被 cmd 误解析，报 `) was unexpected at this time.`；
- **Step 4 改为 Wails 原生 NSIS**（见 4.1，已落地），安装包名从 `wails.json` 动态解析（`name` + `info.productVersion`）；
- `makensis` 检查改为“PATH 里有则用，否则回退 `%ProgramFiles(x86)%\NSIS`”，并顺带修正了原脚本 `set ... & pause` 的解析隐患。

> 说明：总结分支原先用的 `if exist (...) else (...)` 单行写法在 `SETUP_EXE` 未定义/不存在时会让 cmd 解析异常，已改为 label 方案并实测通过。

**`backend/wails.json`**
- `wailsjsdir` 由 `"./wailsjs"` 改为 `"../frontend/angular-app"`。实测后绑定直接生成到 `frontend/angular-app/wailsjs/go/...`（即前端真正 import 的位置），`backend/wailsjs/` 不再产生（已删除该残留目录），从根本上消除绑定副本不同步的隐患；
- 补充 `info.companyName`（原先缺失会让 Wails 生成的 `InstallDir` 出现空公司名层级，`UNINST_KEY_NAME` 也会缺前缀）。

**NSIS 打包（已从手写脚本迁移到 Wails 原生）**
- 删除 `backend/build/nsis/`（`GlobalHotNews.nsi`、`package.bat`）与 `backend/build/GlobalHotNews_Setup.exe`；
- 安装包改由 `wails build -nsis` 产出：`backend/build/bin/global-hot-news-amd64-installer.exe`，**内置 `MicrosoftEdgeWebview2Setup.exe` 引导器**（缺失时静默安装 WebView2 Runtime）；
- 自定义 `backend/build/windows/installer/project.nsi`（Wails 只在缺失时重新生成，因此本地改动会被保留），修掉两个真实缺陷：
  1. **WebView2 缓存目录名对不上**：应用数据在 `%APPDATA%\GlobalHotNews`，而 WebView2 配置在 `%APPDATA%\global-hot-news.exe`；Wails 模板只删后者（能对上），我们要额外删前者的提示逻辑；
  2. **提权卸载时 `$APPDATA` 指向 `C:\ProgramData`**：`wails.setShellContext` 在 admin 安装器下会 `SetShellVarContext all`，导致用户目录根本没被清到。改为在删除用户数据前 `SetShellVarContext current`，并用 `$PROFILE\AppData\Roaming\...`（`$PROFILE` 始终指向发起卸载的用户，即使已提权）；
- 卸载新增“是否删除应用数据”确认框（`/SD IDNO` 作静默默认 = 保留），并补 `RMDir /REBOOTOK $INSTDIR` 处理卸载器自锁。

### 3.4 端到端验证

运行修改后的脚本（去掉 `pause` 的副本）完整跑通：

```
[Step 1/4] Building Angular frontend...        OK
[Step 2/4] Copying frontend to embed directory... OK
[Step 3/4] Building Go backend with Wails...   Built 'backend\build\bin\GlobalHotNews.exe' (14960128 bytes)
[Step 4/4] Creating NSIS installer (wails build -nsis)... Installer created successfully
  Executable: ...\backend\build\bin\GlobalHotNews.exe (14960128 bytes)
  Installer:  ...\backend\build\bin\global-hot-news-amd64-installer.exe (7791127 bytes)
  App name:   global-hot-news
  Version:    1.0.0
  WebView2:   bootstrapper bundled (installs the runtime when missing)
```

退出码 0，无 cmd 解析错误，`backend/main.go` 与 `main.go.buildbak` 均已清理干净。

**安装 / 运行 / 卸载实测**（均用新安装包）：

| 检查项 | 结果 |
|--------|------|
| 安装目录 | `C:\Program Files\Global Hot News\Global Hot News\global-hot-news.exe`，哈希与构建产物一致 |
| 应用启动 + 数据源页面 | 正常渲染真实数据源，`+ Add Source` 可用 |
| 开始菜单快捷方式 | 创建/删除正常（`C:\ProgramData\...\Start Menu\Programs`） |
| 桌面快捷方式 | 创建/删除正常（提权安装 → `C:\Users\Public\Desktop`，属预期） |
| Add/Remove 注册项 | 创建/删除正常，无残留 |
| WebView2 缓存 | 运行后生成 → 卸载后被删除 ✅（修复前会残留） |
| 应用数据 | 静默卸载按 `/SD IDNO` 默认**保留** ✅（修复前因 `$APPDATA` 错位而“碰巧保留”，但删除分支实际不可达） |

> 注：旧的手写安装包注册表键为 `HKLM\...\Uninstall\GlobalHotNews`，新安装包为 `HKLM\...\Uninstall\Global Hot NewsGlobal Hot News`，两者互不干扰。**从旧版本升级前请先卸载旧版本**（旧版本自带 `uninstall.exe`，卸载正常）。

---

## 四、已按建议落地的改造

### 4.1 改用 Wails 原生 NSIS（已实施）

参考项目用的正是这条路：`wails build -nsis`。产出 `backend/build/bin/global-hot-news-amd64-installer.exe`（7.79 MB，比手写安装包大 1.7 MB，多出的就是压缩后的 WebView2 引导器）。相比手写脚本它多了：

- `!insertmacro wails.webview2runtime` —— **自带 WebView2 引导安装**（打包时写入 `build/windows/installer/tmp/MicrosoftEdgeWebview2Setup.exe`，1.8 MB，安装后即清理），目标机没装 WebView2 也能装；
- 正确的 `wails.writeUninstaller` / `deleteUninstaller`、文件关联、自定义协议注册、架构校验（`wails.checkArchitecture`）；
- Add/Remove 注册项与快捷方式随安装上下文正确处理。

已完成的配套改动：
1. `build-desktop.bat` Step 4 替换为 `wails build -platform windows/amd64 -ldflags "-s -w" -nsis`，安装包名从 `wails.json` 动态解析；
2. 删除手写 `backend/build/nsis/`（`GlobalHotNews.nsi`、`package.bat`）与 `backend/build/GlobalHotNews_Setup.exe`；
3. `backend/wails.json` 的 `info` 补上 `companyName`；
4. 自定义 `backend/build/windows/installer/project.nsi`，修掉 Wails 默认模板的两个真实缺陷（WebView2 缓存目录名不匹配、提权卸载时 `$APPDATA` 指向 `C:\ProgramData`），并新增“是否删除应用数据”确认。

> **升级注意**：旧手写安装包注册表键为 `HKLM\...\Uninstall\GlobalHotNews`，新安装包为 `HKLM\...\Uninstall\Global Hot NewsGlobal Hot News`，两者互不干扰。**从旧版本升级前请先运行旧版 `C:\Program Files\GlobalHotNews\uninstall.exe` 卸载旧版本**（旧版卸载器工作正常，且会保留 `%APPDATA%\GlobalHotNews` 用户数据）。

> **维护注意**：`project.nsi` 由 Wails 仅在文件缺失时生成，本地改动会被保留；将来升级 Wails 版本后，建议与 `pkg/buildassets/build/windows/installer/project.nsi` 对比一次，把上游的模板变更（如新的 MUI 页面、签名钩子）合并进来。该文件必须保持 **ASCII 文本且不带 BOM**，否则 NSIS 会误判编码导致编译期语法错误（本次实测：写入中文提示后报 `Error in script "project.nsi" on line 96`）。

### 4.2 其余页面的同类隐患（zoneless）

`pageReady` 是唯一“在 `await` 后置位并作为视图闸门”的状态，所以只有数据源页面必然卡死。但以下页面在 `ngOnInit` 里 `this.loading = true` 也是 `await` 之后才写，**loading 提示永远不会显示**（数据最终仍会出现，因为赋值后没有闸门遮挡）：

- `settings.page.ts` — `loading`
- `recommend.page.ts`、`followed.page.ts`、`hotrank.page.ts`、`finance/military/tech/politics/economy.page.ts` — `loading`

若要彻底消除该模式，两个方向二选一：
- **A（推荐，改动最小）**：按参考项目引入 `zone.js`（`npm i zone.js` + `angular.json` 的 `polyfills: ["zone.js"]`），恢复 zone 语义，所有既有 async 代码无需改动。本次已验证该改动可编译运行（注意：引入后 bundle 目标会降级为 generator，产物名会变，属正常现象）。
- **B（无 zone，面向未来）**：把所有异步后写入的视图状态统一改 signal，即本次数据源页面的做法。

> 当前仓库状态：`zone.js@^0.16.3` 已加入 `frontend/angular-app/package.json` 的 dependencies（Angular 22 把它列为 optional peer，不会冲突），但**尚未**写入 `angular.json` 的 polyfills，因此构建产物仍为 zoneless。要走 A 方案只需在 `angular.json` 的 build options 里加 `"polyfills": ["zone.js"]`；不需要的话也可以 `npm uninstall zone.js` 移除。

---

## 五、数据清理建议

`%APPDATA%\GlobalHotNews\global_hotnews.db` 的 `sources` 表中存在 **104 条空白记录**（`name`/`url` 为空、`is_active=0`、`created_at` 为 `0001-01-01`，id 约 20~133）。它们会让数据源表格出现大量空行。这是历史遗留脏数据，不是当前代码产生的。

清理（先备份数据库）：

```sql
DELETE FROM sources WHERE (name IS NULL OR name = '') AND (url IS NULL OR url = '');
VACUUM;
```

已确认应用运行时不会再产生新的空白行：修复前 `+ Add Source` 因 `pageReady=false` 一直禁用（无法打开表单），修复后表单正常打开且 `saveSource()` 有 name/url 非空校验。

---

## 六、变更清单

| 文件 | 变更 |
|------|------|
| `frontend/angular-app/src/app/pages/datasource/datasource.page.ts` | 状态全部改 signal（修复 Loading 卡死与 Add Source 无响应）；清理无效重试逻辑；`toggleSource` 后刷新列表 |
| `build-desktop.bat` | 修正安装包定位；`main.go` 备份/还原；复制与嵌入校验；总结分支改 label 写法；Step 4 改用 `wails build -nsis`，安装包名动态解析；`makensis` 查找加 PATH 回退 |
| `backend/wails.json` | `wailsjsdir` 指向前端，绑定生成位置与前端引用位置统一；补 `info.companyName` |
| `backend/build/windows/installer/project.nsi` | 自定义 Wails NSIS 模板：删除 WebView2 缓存（按真实目录名）、提权卸载前切回 current 用户上下文并用 `$PROFILE` 定位用户数据、新增删除应用数据确认、`RMDir /REBOOTOK` 兜底 |
| `frontend/angular-app/package.json` | 加入 `zone.js@^0.16.3`（可选，尚未接入 build 的 polyfills，见 4.2） |
| `AUDIT_REPORT.md` / `FIXES_SUMMARY.md` | 新增审计报告；更正原先错误的诊断 |
| **已删除** | `backend/build/nsis/GlobalHotNews.nsi`、`backend/build/nsis/package.bat`（手写 NSIS，已被 Wails 原生方案取代）、`backend/build/GlobalHotNews_Setup.exe`（旧安装包）、`backend/wailsjs/`（绑定生成残留目录） |
