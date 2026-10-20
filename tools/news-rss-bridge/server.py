#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""News RSS Bridge — 无 Docker 依赖的本地 RSS 服务（RSSHub 路由兼容，端口 1200）。

为 ai-informations 后端提供以下路由（与自托管 RSSHub 路径完全一致，
后端 source URL 无需改动）：

    /yicai/brief                    第一财经直播区
    /zhihu/daily                    知乎日报
    /zhihu/weekly                   知乎周刊（已停刊，返回归档说明 feed）
    /caixin/latest                  财新网最新文章
    /sina/finance/rollnews          新浪财经滚动新闻
    /eeo/yaowen/dashi               经济观察网-大宗商品（现行商品快讯栏目）
    /egsea/flash                    e公司快讯（人民财讯）
    /gelonghui/live                 格隆汇快讯
    /zaobao/realtime/china          联合早报-中港台即时
    /eastmoney/report/strategyreport 东方财富策略研报

风格与 tools/china-finance-rss/server.py 保持一致：纯标准库、单文件、带缓存。
"""
import json
import os
import random
import re
import signal
import sys
from datetime import datetime, timedelta, timezone
from email.utils import formatdate
from html import unescape
from http.server import HTTPServer, BaseHTTPRequestHandler
from time import time
from urllib.parse import urlencode, urlparse, parse_qs
from urllib.request import Request, urlopen
from xml.etree import ElementTree as ET

PORT = int(os.getenv('PORT', '1200'))
CACHE_TTL = int(os.getenv('CACHE_TTL', '300'))
REQUEST_TIMEOUT = int(os.getenv('REQUEST_TIMEOUT', '15'))
PUBLIC_BASE_URL = os.getenv('PUBLIC_BASE_URL', '').rstrip('/')

UA = ('Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 '
      '(KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36')
cache = {}

CN_TZ = timezone(timedelta(hours=8))


# ---------------------------------------------------------------- fetch / cache

def fetch(url, headers=None, ttl=None):
    now = time()
    key = url
    if key in cache and now - cache[key]['time'] < (ttl or CACHE_TTL):
        return cache[key]['data']
    h = {'User-Agent': UA}
    if headers:
        h.update(headers)
    req = Request(url, headers=h)
    with urlopen(req, timeout=REQUEST_TIMEOUT) as resp:
        raw = resp.read()
        charset = 'utf-8'
        cd = resp.headers.get('Content-Type', '')
        m = re.search(r'charset=([\w-]+)', cd or '')
        if m:
            charset = m.group(1).lower()
    try:
        data = raw.decode(charset)
    except (LookupError, UnicodeDecodeError):
        data = raw.decode('utf-8', 'replace')
    cache[key] = {'data': data, 'time': now}
    return data


def fetch_json(url, headers=None, ttl=None):
    return json.loads(fetch(url, headers=headers, ttl=ttl))


# ---------------------------------------------------------------- xml helpers

def escape_xml(text):
    return (str(text).replace('&', '&amp;').replace('<', '&lt;').replace('>', '&gt;')
            .replace('"', '&quot;').replace("'", '&apos;'))


def rfc822(ts=None):
    return formatdate(timeval=ts, localtime=False, usegmt=True)


def strip_html(text):
    text = re.sub(r'<[^>]+>', '', str(text or ''))
    return unescape(re.sub(r'\s+', ' ', text)).strip()


def generate_rss(title, link, description, items, feed_url=None):
    xml = (f'<?xml version="1.0" encoding="UTF-8"?>\n'
           f'<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">\n<channel>\n'
           f'<title>{escape_xml(title)}</title>\n<link>{escape_xml(link)}</link>\n'
           f'<description>{escape_xml(description)}</description>\n'
           f'<lastBuildDate>{rfc822()}</lastBuildDate>\n')
    if feed_url:
        xml += f'<atom:link href="{escape_xml(feed_url)}" rel="self" type="application/rss+xml"/>\n'
    for item in items:
        xml += (f'<item>\n<title>{escape_xml(item["title"])}</title>\n'
                f'<link>{escape_xml(item["link"])}</link>\n'
                f'<description>{escape_xml(item["description"])}</description>\n'
                f'<pubDate>{item["pubDate"]}</pubDate>\n'
                f'<guid isPermaLink="false">{escape_xml(item["guid"])}</guid>\n</item>\n')
    return xml + '</channel>\n</rss>'


def generate_error_rss(title, link, description, error, feed_url=None):
    text = str(error) or error.__class__.__name__
    items = [{'title': f'{title} 暂时不可用', 'link': link,
              'description': f'上游抓取失败: {text}',
              'pubDate': rfc822(), 'guid': f'error_{title}_{int(time())}'}]
    return generate_rss(title, link, description, items, feed_url=feed_url)


def generate_opml(base_url):
    base_url = (base_url or '').rstrip('/')
    xml = (f'<?xml version="1.0" encoding="UTF-8"?>\n<opml version="2.0">\n'
           f'<head>\n<title>News RSS Bridge feeds</title>\n</head>\n<body>\n'
           f'<outline text="News RSS Bridge" title="News RSS Bridge">\n')
    for path, (name, _) in ROUTES.items():
        xml += (f'<outline text="{escape_xml(name)}" title="{escape_xml(name)}" '
                f'type="rss" xmlUrl="{escape_xml(base_url + path)}"/>\n')
    return xml + '</outline>\n</body>\n</opml>'


# ---------------------------------------------------------------- 通用 HTML 解析

def parse_cn_datetime(value, fmt='%Y-%m-%d %H:%M:%S'):
    """中国时间字符串 -> UTC 时间戳。"""
    dt = datetime.strptime(value, fmt)
    return dt.replace(tzinfo=CN_TZ).timestamp()


# ---------------------------------------------------------------- handlers

def handle_yicai_brief(feed_url=None):
    url = 'https://www.yicai.com/api/ajax/getbrieflist?' + urlencode(
        {'pagesize': 30, 'type': 0, 'page': 0})
    data = fetch_json(url, headers={'Referer': 'https://www.yicai.com/brief'})
    items = []
    for obj in data:
        link = 'https://www.yicai.com' + obj.get('url', '')
        title = strip_html(obj.get('indexTitle') or obj.get('NewsTitle') or strip_html(obj.get('newcontent', ''))[:60])
        content = strip_html(obj.get('newcontent', ''))
        if obj.get('istop'):
            title = '置顶: ' + title
        try:
            ts = parse_cn_datetime(obj.get('CreateDate', ''), '%Y-%m-%dT%H:%M:%S')
        except (ValueError, TypeError):
            try:
                datekey = (obj.get('datekey') or '').replace('.', '-')
                hm = obj.get('hm') or '00:00'
                ts = parse_cn_datetime(f'{datekey} {hm}:00', '%Y-%m-%d %H:%M:%S')
            except (ValueError, TypeError):
                ts = time()
        items.append({'title': title, 'link': link, 'description': content or title,
                      'pubDate': rfc822(ts), 'guid': f'yicai_{obj.get("id", link)}'})
    return generate_rss('第一财经 - 直播区', 'https://www.yicai.com/brief',
                        '第一财经直播区实时资讯', items, feed_url=feed_url)


def handle_zhihu_daily(feed_url=None):
    url = 'https://news-at.zhihu.com/api/4/news/latest'
    data = fetch_json(url, headers={'Referer': 'https://daily.zhihu.com/'})
    items = []
    now = datetime.now(CN_TZ)
    # stories（当日新闻）+ top_stories（头版），按 id 去重
    seen = set()
    for story in list(data.get('stories', [])) + list(data.get('top_stories', [])):
        sid = story.get('id', '')
        if not sid or sid in seen:
            continue
        seen.add(sid)
        # ga_prefix 形如 10071705 (月日时分)
        ts = None
        gp = str(story.get('ga_prefix') or '')
        if len(gp) >= 8:
            month, day = int(gp[:2]), int(gp[2:4])
            hour, minute = int(gp[4:6]), int(gp[6:8])
            year = now.year
            try:
                cand = datetime(year, month, day, hour, minute, tzinfo=CN_TZ)
                if cand > now:
                    cand = cand.replace(year=year - 1)
                ts = cand.timestamp()
            except ValueError:
                ts = None
        items.append({
            'title': story.get('title', ''),
            'link': f'https://daily.zhihu.com/story/{sid}',
            'description': story.get('hint') or story.get('title', ''),
            'pubDate': rfc822(ts),
            'guid': f'zhihu_daily_{sid}',
        })
    return generate_rss('知乎日报', 'https://daily.zhihu.com/',
                        '知乎日报每日精选', items, feed_url=feed_url)


def handle_zhihu_weekly(feed_url=None):
    """知乎周刊已于 2020 年停刊（最后一期 #295），上游 API 已下线。

    返回一条归档说明 item，保证后端可正常解析且不误报错误。
    """
    items = [{
        'title': '知乎周刊已停刊（最后一期 #295，2020-01-16）',
        'link': 'https://www.zhihu.com/question/374064671',
        'description': ('知乎周刊为知乎出品的免费电子杂志，已于 2020 年 1 月停刊，'
                        '上游 API（news-at.zhihu.com weekly 端点）已下线。'
                        '如需知乎内容，可订阅 /zhihu/daily（知乎日报）。'),
        'pubDate': rfc822(datetime(2020, 1, 16, 12, 0, tzinfo=CN_TZ).timestamp()),
        'guid': 'zhihu_weekly_archived',
    }]
    return generate_rss('知乎周刊（已停刊）', 'https://daily.zhihu.com/',
                        '知乎周刊归档说明', items, feed_url=feed_url)


def handle_caixin_latest(feed_url=None):
    html = fetch('https://www.caixin.com/', headers={'Referer': 'https://www.caixin.com/'})
    pattern = re.compile(r'<a[^>]+href="([^"]*?(\d{4}-\d{2}-\d{2})[^"]*?\.html)"[^>]*>\s*([^<]{6,120}?)\s*</a>')
    items, seen = [], set()
    for m in pattern.finditer(html):
        link, date_str, title = m.group(1), m.group(2), unescape(m.group(3)).strip()
        if link in seen:
            continue
        seen.add(link)
        if link.startswith('/'):
            link = 'https://www.caixin.com' + link
        try:
            ts = parse_cn_datetime(date_str + ' 00:00:00')
        except ValueError:
            ts = time()
        items.append({'title': title, 'link': link, 'description': title,
                      'pubDate': rfc822(ts), 'guid': f'caixin_{link}'})
        if len(items) >= 30:
            break
    if not items:
        raise RuntimeError('财新网首页未解析出文章链接')
    return generate_rss('财新网 - 最新文章', 'https://www.caixin.com/',
                        '财新网最新文章（付费内容仅作更新提醒）', items, feed_url=feed_url)


def handle_sina_rollnews(feed_url=None):
    url = 'https://feed.mix.sina.com.cn/api/roll/get?' + urlencode({
        'pageid': 153, 'lid': 2516, 'k': '', 'num': 50, 'page': 1,
        'r': round(random.random(), 16)})
    data = fetch_json(url, headers={'Referer': 'https://finance.sina.com.cn/'})
    rows = (((data.get('result') or {}).get('data')) or [])
    items = []
    for obj in rows:
        link = obj.get('url') or obj.get('wapurl') or ''
        title = strip_html(obj.get('title', ''))
        if not title or not link:
            continue
        summary = strip_html(obj.get('summary') or obj.get('wapsummary') or '')
        try:
            ts = float(obj.get('ctime') or 0)
        except (TypeError, ValueError):
            ts = time()
        items.append({'title': title, 'link': link, 'description': summary or title,
                      'pubDate': rfc822(ts), 'guid': f'sina_{obj.get("docid", link)}'})
    if not items:
        raise RuntimeError('新浪滚动新闻接口未返回数据')
    return generate_rss('新浪财经 - 滚动新闻', 'https://finance.sina.com.cn/rollnews/',
                        '新浪财经滚动新闻', items, feed_url=feed_url)


def handle_eeo_dashi(feed_url=None):
    """经济观察网。

    原 RSSHub 路由目标 http://www.eeo.com.cn/yaowen/dashi/ 已于 2019 年停更，
    这里改抓现行「快讯 > 商品」栏目（与本项目源名称"大宗商品"对应）。
    """
    url = 'https://www.eeo.com.cn/jg/kuaixun/shangpin/'
    html = fetch(url, headers={'Referer': 'https://www.eeo.com.cn/'})
    items = []
    for block in re.finditer(
            r'<ul class="kx-info-list" data-date="(\d{4}-\d{2}-\d{2})">(.*?)</ul>',
            html, re.S):
        date_str = block.group(1)
        for li in re.finditer(r'<li>(.*?)</li>', block.group(2), re.S):
            body = li.group(1)
            a = re.search(r'<a[^>]+href="([^"]+)"[^>]*>(.*?)</a>', body, re.S)
            if not a:
                continue
            link, title = a.group(1), strip_html(a.group(2))
            if not title:
                continue
            t = re.search(r'<span>\s*([\d:]+)\s*</span>', body)
            time_str = t.group(1) if t else '00:00'
            try:
                ts = parse_cn_datetime(f'{date_str} {time_str}:00', '%Y-%m-%d %H:%M:%S')
            except ValueError:
                ts = time()
            # li 内去掉标题链接后的剩余文本作为描述
            rest = strip_html(body.replace(a.group(0), ''))
            if rest.startswith('经济观察网'):
                rest = rest[len('经济观察网'):].strip()
            items.append({'title': title, 'link': link,
                          'description': rest or title, 'pubDate': rfc822(ts),
                          'guid': f'eeo_{link.rsplit("/", 1)[-1]}'})
    if not items:
        raise RuntimeError('经济观察网商品快讯未解析出条目')
    return generate_rss('经济观察网 - 商品快讯', url,
                        '经济观察网快讯-商品（大宗商品）', items, feed_url=feed_url)


def handle_egsea_flash(feed_url=None):
    url = 'https://www.egsea.com/news/flash-list.html'
    data = fetch_json(url, headers={
        'Referer': 'https://www.egsea.com/news/flash.html',
        'X-Requested-With': 'XMLHttpRequest'})
    items = []
    for obj in data.get('data') or []:
        link = 'https://www.egsea.com' + (obj.get('url') or f'/news/detail/{obj.get("id")}.html')
        title = strip_html(obj.get('title', ''))
        content = strip_html(obj.get('content', ''))
        try:
            ts = float(obj.get('time') or obj.get('show_time') or 0) / 1000
        except (TypeError, ValueError):
            ts = time()
        if ts <= 0:
            ts = time()
        items.append({'title': title or content[:60], 'link': link,
                      'description': content or title,
                      'pubDate': rfc822(ts), 'guid': f'egsea_{obj.get("id", link)}'})
    if not items:
        raise RuntimeError('e公司快讯接口未返回数据')
    return generate_rss('e公司快讯（人民财讯）', 'https://www.egsea.com/news/flash.html',
                        '证券时报·e公司 实时快讯', items, feed_url=feed_url)


# ---- 格隆汇：解析 SSR 页面内嵌的 window.__NUXT__ 数据 ----

def _parse_js_args(s):
    vals, i, n = [], 0, len(s)
    while i < n:
        while i < n and s[i] in ' \n\t\r,':
            i += 1
        if i >= n:
            break
        c = s[i]
        if c in '"\'':
            j, buf = i + 1, []
            while j < n:
                if s[j] == '\\':
                    buf.append(s[j:j + 2])
                    j += 2
                    continue
                if s[j] == c:
                    break
                buf.append(s[j])
                j += 1
            vals.append((c, ''.join(buf)))
            i = j + 1
        else:
            j = i
            while j < n and s[j] != ',':
                j += 1
            vals.append((None, s[i:j].strip()))
            i = j + 1
    return vals


def _js_iife_to_json(html):
    """把 window.__NUXT__=(function(a,..){return {...}})(v1,..) 转成 dict。"""
    m = re.search(r'window\.__NUXT__=\(function\(([^)]*)\)\{return ([\s\S]*?)\}\(([\s\S]*?)\)\);', html)
    if not m:
        raise RuntimeError('格隆汇页面未找到 __NUXT__ 数据')
    params = [p.strip() for p in m.group(1).split(',') if p.strip()]
    body, args_raw = m.group(2), m.group(3)
    args = _parse_js_args(args_raw)
    mapping = {}
    for p, (q, v) in zip(params, args):
        if q in ('"', "'"):
            mapping[p] = json.dumps(v)
        elif v in ('true', 'false', 'null'):
            mapping[p] = v
        elif re.fullmatch(r'-?\d+(\.\d+)?', v or ''):
            mapping[p] = v
        else:
            mapping[p] = 'null'

    src = re.sub(r'new Date\((-?\d+)\)', r'\1', body)
    src = re.sub(r'\bundefined\b', 'null', src)
    out, i, n = [], 0, len(src)
    while i < n:
        c = src[i]
        if c in '"\'':
            j, buf = i + 1, []
            while j < n:
                if src[j] == '\\':
                    buf.append(src[j:j + 2])
                    j += 2
                    continue
                if src[j] == c:
                    break
                buf.append(src[j])
                j += 1
            s = ''.join(buf).replace('\\/', '/')
            if c == "'":
                s = s.replace('\\', '\\\\').replace('"', '\\"')
            out.append('"' + s + '"')
            i = j + 1
            continue
        if c == '`':
            j, buf = i + 1, []
            while j < n:
                if src[j] == '\\':
                    buf.append(src[j:j + 2])
                    j += 2
                    continue
                if src[j] == '`':
                    break
                buf.append(src[j])
                j += 1
            s = ''.join(buf).replace('\\/', '/').replace('"', '\\"')
            out.append('"' + s + '"')
            i = j + 1
            continue
        if c.isalpha() or c in '_$':
            j = i
            while j < n and (src[j].isalnum() or src[j] in '_$'):
                j += 1
            ident = src[i:j]
            out.append(mapping.get(ident, ident))
            i = j
            continue
        out.append(c)
        i += 1
    conv = re.sub(r'([{,\[]\s*)([A-Za-z_$][\w$]*)(\s*:)', r'\1"\2"\3', ''.join(out))
    return json.loads(conv)


def handle_gelonghui_live(feed_url=None):
    html = fetch('https://www.gelonghui.com/live', headers={'Referer': 'https://www.gelonghui.com/'})
    data = _js_iife_to_json(html)
    rows = ((data.get('data') or [{}])[0].get('data')) or []
    items = []
    for obj in rows:
        oid = obj.get('id', '')
        title = strip_html(obj.get('title') or strip_html(obj.get('content', ''))[:60])
        content = strip_html(obj.get('content', ''))
        try:
            ts = float(obj.get('createTimestamp') or 0)
        except (TypeError, ValueError):
            ts = time()
        if ts <= 0:
            ts = time()
        items.append({'title': title, 'link': f'https://www.gelonghui.com/article/{oid}',
                      'description': content or title, 'pubDate': rfc822(ts),
                      'guid': f'gelonghui_{oid}'})
    if not items:
        raise RuntimeError('格隆汇快讯未解析出条目')
    return generate_rss('格隆汇快讯', 'https://www.gelonghui.com/live',
                        '格隆汇 7x24 小时市场快讯', items, feed_url=feed_url)


def _parse_zaobao_relative(value, now_ts):
    """联合早报列表页的相对时间（如 '12分钟前'）转时间戳。"""
    v = (value or '').strip()
    m = re.match(r'(\d+)\s*秒前', v)
    if m:
        return now_ts - int(m.group(1))
    m = re.match(r'(\d+)\s*分钟前', v)
    if m:
        return now_ts - int(m.group(1)) * 60
    m = re.match(r'(\d+)\s*小时前', v)
    if m:
        return now_ts - int(m.group(1)) * 3600
    m = re.match(r'(\d+)\s*天前', v)
    if m:
        return now_ts - int(m.group(1)) * 86400
    m = re.match(r'(\d{1,2})月(\d{1,2})日\s*(\d{1,2})?[:：]?(\d{2})?', v)
    if m:
        year = datetime.now(CN_TZ).year
        month, day = int(m.group(1)), int(m.group(2))
        hour = int(m.group(3) or 0)
        minute = int(m.group(4) or 0)
        try:
            ts = datetime(year, month, day, hour, minute, tzinfo=CN_TZ).timestamp()
            if ts > now_ts:
                ts -= 365 * 86400
            return ts
        except ValueError:
            pass
    return now_ts


def handle_zaobao_china(feed_url=None):
    url = 'https://www.zaobao.com/realtime/china'
    html = fetch(url, headers={'Referer': 'https://www.zaobao.com/'})
    now_ts = time()
    items = []
    for m in re.finditer(r'<a([^>]*class="[^"]*article-link[^"]*"[^>]*)>(.*?)</a>', html, re.S):
        attrs = m.group(1)
        href = re.search(r'href="([^"]+)"', attrs)
        title_attr = re.search(r'title="([^"]+)"', attrs)
        if not href:
            continue
        link = href.group(1)
        if link.startswith('/'):
            link = 'https://www.zaobao.com' + link
        title = unescape(title_attr.group(1) if title_attr else strip_html(m.group(2)))
        if not title:
            continue
        # 该链接之后最近的一个 <span>...（相对时间）
        tail = html[m.end():m.end() + 400]
        t = re.search(r'<span>\s*([^<]+?)\s*</span>', tail)
        ts = _parse_zaobao_relative(t.group(1) if t else '', now_ts)
        items.append({'title': title, 'link': link, 'description': title,
                      'pubDate': rfc822(ts), 'guid': f'zaobao_{link.rsplit("/", 1)[-1]}'})
    if not items:
        raise RuntimeError('联合早报页面未解析出条目')
    return generate_rss('《联合早报》-中港台-即时', 'https://www.zaobao.com/realtime/china',
                        '联合早报即时新闻（中港台）', items, feed_url=feed_url)


def handle_eastmoney_strategy(feed_url=None):
    url = 'https://reportapi.eastmoney.com/report/list?' + urlencode({
        'industryCode': '*', 'pageSize': 30, 'industry': '*', 'rating': '*',
        'ratingChange': '*', 'beginTime': '2025-01-01', 'endTime': '2026-12-31',
        'pageNo': 1, 'fields': '', 'qType': 2})
    data = fetch_json(url, headers={'Referer': 'https://data.eastmoney.com/report/'})
    items = []
    for obj in data.get('data') or []:
        code = obj.get('infoCode', '')
        link = f'https://data.eastmoney.com/report/zw_stock.jshtml?infoCode={code}'
        title = strip_html(obj.get('title', ''))
        if not title:
            continue
        org = obj.get('orgSName') or ''
        stock = obj.get('stockName') or ''
        desc = ' / '.join(x for x in (org, stock) if x) + f' | {obj.get("publishDate", "")[:10]}'
        try:
            ts = parse_cn_datetime(str(obj.get('publishDate', ''))[:19])
        except ValueError:
            ts = time()
        items.append({'title': title, 'link': link, 'description': f'{title}\n{desc.strip()}',
                      'pubDate': rfc822(ts), 'guid': f'em_report_{code}'})
    if not items:
        raise RuntimeError('东方财富研报接口未返回数据')
    return generate_rss('东方财富 - 策略研报', 'https://data.eastmoney.com/report/strategyreport.jshtml',
                        '东方财富研究中心策略/宏观研报', items, feed_url=feed_url)


# ---------------------------------------------------------------- routes

ROUTES = {
    '/yicai/brief': ('Yicai Brief (第一财经直播)', handle_yicai_brief),
    '/zhihu/daily': ('Zhihu Daily (知乎日报)', handle_zhihu_daily),
    '/zhihu/weekly': ('Zhihu Weekly (知乎周刊-已停刊)', handle_zhihu_weekly),
    '/caixin/latest': ('Caixin Latest (财新网最新文章)', handle_caixin_latest),
    '/sina/finance/rollnews': ('Sina Finance Rollnews (新浪财经滚动)', handle_sina_rollnews),
    '/eeo/yaowen/dashi': ('EEO Commodity Flash (经济观察网-大宗商品)', handle_eeo_dashi),
    '/egsea/flash': ('e Company Flash (e公司快讯)', handle_egsea_flash),
    '/gelonghui/live': ('Gelonghui Live (格隆汇快讯)', handle_gelonghui_live),
    '/zaobao/realtime/china': ('Zaobao China (联合早报-中港台)', handle_zaobao_china),
    '/eastmoney/report/strategyreport': ('Eastmoney Strategy Report (东方财富策略研报)', handle_eastmoney_strategy),
}
ROUTE_META = {
    '/yicai/brief': ('第一财经 - 直播区', 'https://www.yicai.com/brief', '第一财经直播区实时资讯'),
    '/zhihu/daily': ('知乎日报', 'https://daily.zhihu.com/', '知乎日报每日精选'),
    '/zhihu/weekly': ('知乎周刊（已停刊）', 'https://daily.zhihu.com/', '知乎周刊归档说明'),
    '/caixin/latest': ('财新网 - 最新文章', 'https://www.caixin.com/', '财新网最新文章'),
    '/sina/finance/rollnews': ('新浪财经 - 滚动新闻', 'https://finance.sina.com.cn/rollnews/', '新浪财经滚动新闻'),
    '/eeo/yaowen/dashi': ('经济观察网 - 商品快讯', 'https://www.eeo.com.cn/jg/kuaixun/shangpin/', '经济观察网快讯-商品'),
    '/egsea/flash': ('e公司快讯', 'https://www.egsea.com/news/flash.html', '证券时报·e公司 实时快讯'),
    '/gelonghui/live': ('格隆汇快讯', 'https://www.gelonghui.com/live', '格隆汇 7x24 小时市场快讯'),
    '/zaobao/realtime/china': ('《联合早报》-中港台-即时', 'https://www.zaobao.com/realtime/china', '联合早报即时新闻'),
    '/eastmoney/report/strategyreport': ('东方财富 - 策略研报', 'https://data.eastmoney.com/report/strategyreport.jshtml', '东方财富策略/宏观研报'),
}


def count_rss_items(xml):
    root = ET.fromstring(xml)
    return len(root.findall('./channel/item'))


def build_health_payload(base_url, check_sources=False):
    feeds = []
    status = 'ok'
    for path, (name, handler) in ROUTES.items():
        entry = {'name': name, 'path': path, 'url': base_url + path, 'status': 'configured'}
        if check_sources:
            try:
                xml = handler(feed_url=base_url + path)
                entry['status'] = 'ok'
                entry['items'] = count_rss_items(xml)
            except Exception as exc:
                entry['status'] = 'error'
                entry['error'] = str(exc)
                status = 'degraded'
        feeds.append(entry)
    return {'status': status, 'cache_ttl': CACHE_TTL, 'request_timeout': REQUEST_TIMEOUT,
            'feeds': feeds}


# ---------------------------------------------------------------- http server

class RSSHandler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        sys.stderr.write(f'[{self.log_date_time_string()}] {fmt % args}\n')

    def do_HEAD(self):
        self._handle_request(write_body=False)

    def do_GET(self):
        self._handle_request(write_body=True)

    def _handle_request(self, write_body=True):
        parsed = urlparse(self.path)
        path = parsed.path
        base_url = self._base_url()
        if path == '/':
            self._serve_index(write_body=write_body)
            return
        if path == '/opml.xml':
            self._send_text(200, 'text/x-opml; charset=utf-8', generate_opml(base_url),
                            write_body=write_body)
            return
        if path == '/healthz':
            query = parse_qs(parsed.query)
            check = query.get('check', ['0'])[0] in ('1', 'true', 'yes')
            payload = build_health_payload(base_url, check_sources=check)
            code = 503 if payload['status'] == 'degraded' else 200
            self._send_text(code, 'application/json; charset=utf-8',
                            json.dumps(payload, ensure_ascii=False, indent=2),
                            cache=False, write_body=write_body)
            return
        if path in ROUTES:
            self._serve_feed(path, base_url, write_body=write_body)
            return
        self.send_error(404, 'Not Found. Visit / for available feeds.')

    def _serve_feed(self, path, base_url, write_body=True):
        _, handler = ROUTES[path]
        feed_url = base_url + path
        try:
            xml = handler(feed_url=feed_url)
        except Exception as exc:
            title, link, description = ROUTE_META[path]
            xml = generate_error_rss(title, link, description, exc, feed_url=feed_url)
        self._send_text(200, 'application/rss+xml; charset=utf-8', xml, write_body=write_body)

    def _base_url(self):
        if PUBLIC_BASE_URL:
            return PUBLIC_BASE_URL
        proto = self.headers.get('X-Forwarded-Proto', 'http').split(',')[0].strip()
        host = self.headers.get('X-Forwarded-Host') or self.headers.get('Host')
        return f'{proto}://{host or f"localhost:{PORT}"}'.rstrip('/')

    def _send_text(self, status_code, content_type, body, cache=True, write_body=True):
        body_bytes = body.encode('utf-8')
        self.send_response(status_code)
        self.send_header('Content-Type', content_type)
        self.send_header('Content-Length', str(len(body_bytes)))
        if cache:
            self.send_header('Cache-Control', f'public, max-age={CACHE_TTL}')
        self.end_headers()
        if write_body:
            self.wfile.write(body_bytes)

    def _serve_index(self, write_body=True):
        lines = ['<html><head><title>News RSS Bridge</title></head><body>',
                 '<h1>News RSS Bridge</h1><ul>']
        for path, (name, _) in ROUTES.items():
            lines.append(f'<li><a href="{path}">{name}</a></li>')
        lines.append('</ul><p><a href="/opml.xml">Import OPML</a> | '
                     '<a href="/healthz?check=1">Source check</a></p>')
        lines.append('<p>Drop-in replacement for the Docker RSSHub routes on this port.</p>'
                     '</body></html>')
        self._send_text(200, 'text/html; charset=utf-8', '\n'.join(lines),
                        cache=False, write_body=write_body)


def main():
    signal.signal(signal.SIGINT, lambda *_: sys.exit(0))
    signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
    print(f'News RSS Bridge running on http://localhost:{PORT}')
    print(f'Cache TTL: {CACHE_TTL}s | Timeout: {REQUEST_TIMEOUT}s\n')
    print('Available feeds:')
    for path, (name, _) in ROUTES.items():
        print(f'  http://localhost:{PORT}{path}  — {name}')
    print(f'\nUtilities:')
    print(f'  http://localhost:{PORT}/opml.xml  — OPML subscription list')
    print(f'  http://localhost:{PORT}/healthz?check=1  — Source health check\n')
    server = HTTPServer(('0.0.0.0', PORT), RSSHandler)
    server.serve_forever()


if __name__ == '__main__':
    main()
