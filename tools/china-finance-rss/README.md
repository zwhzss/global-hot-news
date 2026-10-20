# China Finance RSS Bridge

Lightweight RSS bridge for Chinese financial news.

## Feeds

| Route | Source |
|-------|--------|
| `/cls/telegraph` | 财联社电报 |
| `/eastmoney/kuaixun` | 东方财富快讯 |
| `/ths/kuaixun` | 同花顺快讯 |
| `/jin10/flash` | 金十快讯 |
| `/wallstreetcn/live` | 华尔街见闻快讯 |
| `/xueqiu/user/{uid}` | 雪球用户 (requires Chrome CDP) |

## Usage

```bash
python server.py
```

Access: http://localhost:8053/

## Environment Variables

- `PORT` - Server port (default: 8053)
- `CDP_URL` - Chrome DevTools Protocol URL (default: http://localhost:9222)
- `CACHE_TTL` - Cache TTL in seconds (default: 300)
- `REQUEST_TIMEOUT` - Request timeout (default: 10)
