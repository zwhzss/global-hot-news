"""A local OpenAI-compatible translation stub — a TEST TOOL, not part of the app.

The application never talks to this file. It exists so the bilingual pipeline can
be exercised end to end (settings -> liveness probe -> worker -> SQLite -> the
language tabs on an article card) without downloading a multi-gigabyte local
model first. It is also what the automated verification used.

It answers the two endpoints the translator needs:
    GET  /v1/models             -> a fake model list (also the liveness probe)
    POST /v1/chat/completions   -> a glossary-based pseudo translation

The output is deliberately obvious ("[EN] ..." / "[中译] ..." when the glossary
misses) so nobody mistakes it for a real translation.

Usage:
    1. python tools/stub_translator.py 18080
    2. In the app: Settings -> Bilingual Translation
         provider : Local model (free)
         base URL : http://127.0.0.1:18080/v1
         model    : qwen2.5:7b
       then Save, Test connection, Translate pending now.

For real use, point the app at Ollama or LM Studio instead.
"""

import json
import re
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 18080

# Small enough to read, big enough to make news headlines look plausible.
EN2ZH = {
    "central bank": "央行", "fed": "美联储", "federal reserve": "美联储",
    "interest rate": "利率", "interest rates": "利率", "rates": "利率",
    "inflation": "通胀", "economy": "经济", "economic": "经济",
    "stock": "股市", "stocks": "股市", "market": "市场", "markets": "市场",
    "oil": "原油", "gold": "黄金", "china": "中国", "chinese": "中国",
    "united states": "美国", "u.s.": "美国", "us": "美国", "russia": "俄罗斯",
    "ukraine": "乌克兰", "war": "战争", "military": "军事", "defense": "国防",
    "ai": "人工智能", "chip": "芯片", "chips": "芯片", "semiconductor": "半导体",
    "technology": "科技", "company": "公司", "government": "政府",
    "president": "总统", "minister": "部长", "trade": "贸易", "tariff": "关税",
    "growth": "增长", "report": "报告", "says": "表示", "said": "表示",
    "new": "新", "year": "年", "week": "周", "day": "日", "up": "上涨",
    "down": "下跌", "rise": "上升", "fall": "下降", "talks": "会谈",
    "election": "选举", "energy": "能源", "climate": "气候", "bank": "银行",
    "quarter": "季度", "sales": "销售", "profit": "利润", "billion": "十亿",
    "million": "百万", "percent": "百分比", "policy": "政策", "data": "数据",
}
ZH2EN = {
    "央行": "central bank", "利率": "interest rates", "通胀": "inflation",
    "经济": "economy", "股市": "stock market", "原油": "crude oil",
    "黄金": "gold", "中国": "China", "美国": "the United States",
    "俄罗斯": "Russia", "乌克兰": "Ukraine", "军事": "military",
    "国防": "defense", "人工智能": "AI", "芯片": "chips", "科技": "technology",
    "公司": "company", "政府": "government", "总统": "president",
    "贸易": "trade", "关税": "tariffs", "增长": "growth", "报告": "report",
    "表示": "said", "选举": "election", "能源": "energy", "气候": "climate",
    "银行": "bank", "政策": "policy", "数据": "data", "季度": "quarter",
    "销售": "sales", "利润": "profit", "下降": "fell", "上升": "rose",
}


def translate_en2zh(text: str) -> str:
    out = text
    for en, zh in sorted(EN2ZH.items(), key=lambda kv: -len(kv[0])):
        out = re.sub(re.escape(en), zh, out, flags=re.IGNORECASE)
    return out if out != text else "[中译] " + text


def translate_zh2en(text: str) -> str:
    out = text
    for zh, en in sorted(ZH2EN.items(), key=lambda kv: -len(kv[0])):
        out = out.replace(zh, " " + en + " ")
    out = re.sub(r"\s+", " ", out).strip()
    return out if out != text else "[EN] " + text


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        sys.stderr.write("[stub-translator] " + (fmt % args) + "\n")

    def _json(self, payload):
        body = json.dumps(payload).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path.rstrip("/").endswith("/models"):
            self._json({"object": "list", "data": [
                {"id": "qwen2.5:7b", "object": "model"},
                {"id": "stub-translator", "object": "model"},
            ]})
        else:
            self.send_error(404)

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        req = json.loads(self.rfile.read(length) or b"{}")
        messages = req.get("messages", [])
        system = messages[0].get("content", "") if messages else ""
        texts = json.loads(messages[1].get("content", "[]")) if len(messages) > 1 else []
        to_chinese = "Simplified Chinese" in system

        out = [translate_en2zh(t) if to_chinese else translate_zh2en(t) for t in texts]
        self._json({
            "id": "stub", "object": "chat.completion", "model": req.get("model", "stub"),
            "choices": [{
                "index": 0, "finish_reason": "stop",
                "message": {"role": "assistant", "content": json.dumps(out, ensure_ascii=False)},
            }],
        })


if __name__ == "__main__":
    print(f"[stub-translator] listening on http://127.0.0.1:{PORT}/v1", flush=True)
    ThreadingHTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
