# News RSS Bridge

无 Docker 依赖的本地 RSS 服务，替代原先用 Docker 跑的 RSSHub（localhost:1200），
为 ai-informations 后端提供 RSSHub 兼容路由（路径完全一致，后端 source URL 无需改动）。

## Feeds

| Route | Source | 上游方式 |
|-------|--------|----------|
| `/yicai/brief` | 第一财经直播区 | JSON API |
| `/zhihu/daily` | 知乎日报 | JSON API（news-at.zhihu.com） |
| `/zhihu/weekly` | 知乎周刊（已停刊，返回归档说明） | 静态 |
| `/caixin/latest` | 财新网最新文章 | 首页 HTML |
| `/sina/finance/rollnews` | 新浪财经滚动新闻 | JSON API（feed.mix.sina.com.cn） |
| `/eeo/yaowen/dashi` | 经济观察网-大宗商品 | 现行「快讯>商品」栏目 HTML（原 yaowen/dashi 栏目 2019 年停更） |
| `/egsea/flash` | e公司快讯（人民财讯） | JSON（/news/flash-list.html） |
| `/gelonghui/live` | 格隆汇快讯 | SSR 页面 `window.__NUXT__` 内嵌数据 |
| `/zaobao/realtime/china` | 联合早报-中港台即时 | 页面 HTML |
| `/eastmoney/report/strategyreport` | 东方财富策略研报 | JSON API（reportapi.eastmoney.com，qType=2） |

## Usage

```bat
start-bridge.bat    rem 启动（默认端口 1200，已在运行则跳过）
stop-bridge.bat     rem 停止
python server.py    rem 前台运行（调试）
```

- 首页: http://localhost:1200/
- OPML: http://localhost:1200/opml.xml
- 健康检查: http://localhost:1200/healthz?check=1

## Environment Variables

- `PORT` - 服务端口（默认 1200，与后端种子 source URL 匹配）
- `CACHE_TTL` - 上游抓取缓存秒数（默认 300）
- `REQUEST_TIMEOUT` - 上游请求超时秒数（默认 15）
- `PUBLIC_BASE_URL` - 生成 feed 自链接时使用的外部基础 URL（可选）

## Notes

- 纯 Python 标准库实现，无第三方依赖（与 `tools\china-finance-rss` 风格一致）。
- 若需恢复 Docker 版 RSSHub，请先停止本服务释放 1200 端口。
