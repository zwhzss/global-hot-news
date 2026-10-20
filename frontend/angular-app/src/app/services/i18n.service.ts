import { Injectable, computed, signal } from '@angular/core';

export type Lang = 'zh' | 'en';

/**
 * UI language.
 *
 * Every board (recommend, follows, hot rank and the five topic boards) renders
 * its labels through `t()`, and the switch in the sidebar flips the whole app.
 * The choice is persisted so it survives a restart.
 *
 * The dictionary lives in code rather than in a JSON asset on purpose: the app
 * is a local single-user desktop tool with exactly two languages, so a runtime
 * loader would add a failure mode (missing file -> blank UI) with no benefit.
 */
const STORAGE_KEY = 'ghn.lang';

type Entry = { zh: string; en: string };

const DICT: Record<string, Entry> = {
  // ---- navigation / board titles ----
  'nav.recommend': { zh: '推荐', en: 'Recommend' },
  'nav.followed': { zh: '我的关注', en: 'My Follows' },
  'nav.hotrank': { zh: '热榜', en: 'Hot Rankings' },
  'nav.finance': { zh: '财经', en: 'Finance' },
  'nav.military': { zh: '军事', en: 'Military' },
  'nav.tech': { zh: '科技', en: 'Technology' },
  'nav.politics': { zh: '政治', en: 'Politics' },
  'nav.economy': { zh: '经济', en: 'Economy' },
  'nav.datasources': { zh: '数据源', en: 'Data Sources' },
  'nav.settings': { zh: '设置', en: 'Settings' },
  'nav.tasks': { zh: '任务监控', en: 'Tasks' },

  // ---- common ----
  'common.loading': { zh: '加载中…', en: 'Loading…' },
  'common.prev': { zh: '上一页', en: 'Prev' },
  'common.next': { zh: '下一页', en: 'Next' },
  'common.pageOf': { zh: '第 {{page}} / {{total}} 页', en: 'Page {{page}} of {{total}}' },
  'common.totalCount': { zh: '共 {{total}} 条', en: '{{total}} articles' },
  'common.reset': { zh: '重置', en: 'Reset' },
  'common.all': { zh: '全部', en: 'All' },
  'common.yes': { zh: '是', en: 'Yes' },
  'common.no': { zh: '否', en: 'No' },
  'common.retry': { zh: '重试', en: 'Retry' },
  'common.close': { zh: '关闭', en: 'Close' },
  'common.save': { zh: '保存', en: 'Save' },
  'common.cancel': { zh: '取消', en: 'Cancel' },
  'common.showingToday': { zh: '默认只显示当天文章', en: 'Showing today only by default' },

  // ---- article content language ----
  'content.zh': { zh: '中文', en: '中文' },
  'content.en': { zh: 'English', en: 'English' },
  'content.original': { zh: '原文', en: 'Original' },
  'content.translation': { zh: '译文', en: 'Translation' },
  'content.pending': { zh: '该语言的译文尚未生成', en: 'No translation in this language yet' },
  'content.unavailable': {
    zh: '翻译服务未启用（设置 → 双语翻译）',
    en: 'Translation is off (Settings → Bilingual)',
  },
  'content.translateNow': { zh: '立即翻译', en: 'Translate now' },
  'content.translating': { zh: '翻译中…', en: 'Translating…' },
  'content.showOriginal': { zh: '显示原文', en: 'Show original' },

  // ---- filter bar ----
  'filter.date': { zh: '日期', en: 'Date' },
  'filter.today': { zh: '今天', en: 'Today' },
  'filter.last7': { zh: '近 7 天', en: 'Last 7 days' },
  'filter.last30': { zh: '近 30 天', en: 'Last 30 days' },
  'filter.allDates': { zh: '全部日期', en: 'All dates' },
  'filter.custom': { zh: '自定义', en: 'Custom' },
  'filter.from': { zh: '开始', en: 'From' },
  'filter.to': { zh: '结束', en: 'To' },
  'filter.title': { zh: '标题', en: 'Title' },
  'filter.titlePlaceholder': { zh: '标题关键字（中英文均可）', en: 'Title keyword (Chinese or English)' },
  'filter.sort': { zh: '排序', en: 'Sort' },
  'filter.sortTime': { zh: '按时间倒序', en: 'Newest first' },
  'filter.sortHot': { zh: '按热度', en: 'By heat' },
  'filter.apply': { zh: '查询', en: 'Search' },
  'filter.availableDates': { zh: '有数据的日期', en: 'Dates with data' },
  'filter.resultCount': { zh: '命中 {{total}} 条', en: '{{total}} matches' },

  // ---- empty states ----
  'empty.recommend': {
    zh: '暂无推荐内容。浏览文章后会逐步形成你的个人画像。',
    en: 'No recommendations yet. Start browsing to build your profile.',
  },
  'empty.followed': { zh: '还没有关注的资讯。点击文章上的 ☆ 即可关注。', en: 'No followed articles yet. Star articles to follow them.' },
  'empty.hotrank': { zh: '暂无热榜数据。采集到文章后会自动生成。', en: 'No hot rankings yet. Articles will appear here once collected.' },
  'empty.finance': { zh: '当前条件下没有财经资讯。', en: 'No finance news for the current filter.' },
  'empty.military': { zh: '当前条件下没有军事资讯。', en: 'No military news for the current filter.' },
  'empty.tech': { zh: '当前条件下没有科技资讯。', en: 'No technology news for the current filter.' },
  'empty.politics': { zh: '当前条件下没有政治资讯。', en: 'No politics news for the current filter.' },
  'empty.economy': { zh: '当前条件下没有经济资讯。', en: 'No economy news for the current filter.' },
  'empty.sources': { zh: '没有关注的数据源', en: 'No followed sources' },

  // ---- article card ----
  'card.follow': { zh: '☆ 关注', en: '☆ Follow' },
  'card.unfollow': { zh: '★ 已关注', en: '★ Following' },
  'card.justNow': { zh: '刚刚', en: 'Just now' },
  'card.minutesAgo': { zh: '{{n}} 分钟前', en: '{{n}}m ago' },
  'card.hoursAgo': { zh: '{{n}} 小时前', en: '{{n}}h ago' },
  'card.daysAgo': { zh: '{{n}} 天前', en: '{{n}}d ago' },
  'card.unknown': { zh: '未知', en: 'Unknown' },

  // ---- followed board ----
  'followed.tabArticles': { zh: '关注的资讯', en: 'Followed Articles' },
  'followed.tabSources': { zh: '关注的数据源', en: 'Followed Sources' },
  'followed.name': { zh: '名称', en: 'Name' },
  'followed.type': { zh: '类型', en: 'Type' },
  'followed.status': { zh: '状态', en: 'Status' },
  'followed.url': { zh: '地址', en: 'URL' },
  'followed.actions': { zh: '操作', en: 'Actions' },
  'followed.unfollow': { zh: '取消关注', en: 'Unfollow' },

  // ---- hot rank board ----
  'hotrank.recalculate': { zh: '重新计算', en: 'Recalculate' },
  'hotrank.score': { zh: '热度', en: 'Score' },

  // ---- recommend board ----
  'recommend.resetProfile': { zh: '重置画像', en: 'Reset Profile' },
  'recommend.resetConfirm': {
    zh: '确定要重置推荐画像吗？浏览历史将被清空。',
    en: 'Reset your recommendation profile? This will clear all browsing history.',
  },

  // ---- narration player (TTS round playback) ----
  'player.blocked': { zh: '已拦截自动播放，点击播放按钮开始', en: 'Auto-play blocked — press play to start' },
  'player.synthesizing': { zh: '合成中', en: 'Synthesising' },
  'player.noRound': { zh: '本轮文章朗读就绪后将自动播放', en: 'Auto-plays once this round finishes' },
  'player.play': { zh: '播放', en: 'Play' },
  'player.pause': { zh: '暂停', en: 'Pause' },
  'player.stop': { zh: '停止', en: 'Stop' },
  'player.noAudio': { zh: '本轮还没有可播放的音频', en: 'No audio ready for this round yet' },

  // ---- settings: TTS narration ----
  'tts.section': { zh: '语音合成（朗读）', en: 'Text-to-Speech (Narration)' },
  'tts.hint': {
    zh: '每轮拉取结束后，自动为本轮新文章生成朗读音频（edge-tts 微软在线语音），并在热榜、推荐页自动播放本轮列表。',
    en: 'After each collection round, narration audio is generated for the round\'s new articles (edge-tts, Microsoft online voices) and auto-plays on the hot-rank and recommendation boards.',
  },
  'tts.enabled': { zh: '启用语音合成', en: 'Enable TTS' },
  'tts.engine': { zh: '合成引擎', en: 'Synthesis engine' },
  'tts.engineEdge': { zh: 'edge-tts（免费）', en: 'edge-tts (free)' },
  'tts.engineAPI': { zh: '商用大模型 API', en: 'Commercial LLM API' },
  'tts.apiUrl': { zh: 'API 地址', en: 'API base URL' },
  'tts.apiUrlHint': {
    zh: 'OpenAI 兼容 TTS 接口，例如 https://api.openai.com/v1；请求 POST <地址>/audio/speech。',
    en: 'OpenAI-compatible TTS endpoint, e.g. https://api.openai.com/v1; requests POST <url>/audio/speech.',
  },
  'tts.apiKey': { zh: 'API Key', en: 'API key' },
  'tts.apiModel': { zh: '模型', en: 'Model' },
  'tts.apiVoice': { zh: '音色', en: 'Voice' },
  'tts.voice': { zh: '语音', en: 'Voice' },
  'tts.maxPerRound': { zh: '每轮最多合成（篇）', en: 'Max articles per round' },
  'tts.concurrency': { zh: '并发合成数', en: 'Parallel workers' },
  'tts.concurrencyHint': {
    zh: '同时执行的合成任务数 1-8（每任务一个隐藏的 python 进程，越大越快也越占带宽）。',
    en: 'Simultaneous synthesis tasks, 1-8 (each spawns one hidden python process; higher = faster but more bandwidth).',
  },
  'tts.autoPlay': { zh: '本轮完成后自动播放', en: 'Auto-play when a round finishes' },
  'tts.python': { zh: 'Python 路径', en: 'Python executable' },
  'tts.pythonHint': {
    zh: '留空自动探测；需已安装 edge-tts（pip install edge-tts）。',
    en: 'Empty = auto-detect; edge-tts must be installed (pip install edge-tts).',
  },
  'tts.test': { zh: '测试合成', en: 'Test synthesis' },
  'tts.testing': { zh: '合成中...', en: 'Synthesising...' },
  'tts.ok': { zh: '合成成功，可试听：', en: 'Synthesis succeeded — listen below:' },
  'tts.synthesizeNow': { zh: '合成音频', en: 'Synthesize audio' },
  'tts.synthesizing': { zh: '合成中…', en: 'Synthesising…' },

  // ---- settings ----
  'settings.title': { zh: '系统设置', en: 'System Settings' },
  'settings.maxConcurrency': { zh: '采集并发数', en: 'Max Collector Concurrency' },
  'settings.defaultInterval': { zh: '默认请求间隔（秒）', en: 'Default Request Interval (seconds)' },
  'settings.articleTtl': { zh: '文章保留天数', en: 'Article TTL (days)' },
  'settings.logLevel': { zh: '日志级别', en: 'Log Level' },
  'settings.complianceFilter': { zh: '合规过滤', en: 'Compliance Filter' },
  'settings.robots': { zh: '遵守 robots.txt', en: 'Robots.txt Enforcement' },
  'settings.autoCollect': { zh: '启动时自动采集', en: 'Auto Collect on Startup' },
  'settings.saved': {
    zh: '设置已保存。部分改动需要重启应用后生效。',
    en: 'Settings saved. Some changes take effect after a restart.',
  },
  'settings.about': { zh: '关于', en: 'About' },
  'settings.aboutText': {
    zh: 'Global Hot News — 本地个人资讯聚合工具。只采集公开的事实性资讯，本地只保存标题与摘要，全部数据留在本机，不分享、不上传、不商用。',
    en: 'Global Hot News — a local personal news aggregator. It only collects public fact-based news, stores only titles and abstracts locally, and keeps all data on this machine. No sharing, uploading or commercial use.',
  },

  // ---- classification model settings ----
  'cls.section': { zh: '分类模型', en: 'Classification model' },
  'cls.hint': {
    zh: '分类使用独立的模型配置，与下方翻译模型互不影响；修改后立即生效，无需重启。接口地址留空则分类只用关键词匹配。',
    en: 'The classifier uses its own model settings, fully independent from the translation model below; changes apply immediately, no restart needed. Leave the URL empty to classify with keywords only.',
  },
  'cls.baseUrl': { zh: '模型接口地址', en: 'Model base URL' },
  'cls.model': { zh: '模型名称', en: 'Model' },
  'cls.apiKey': { zh: 'API Key（本地模型留空）', en: 'API key (leave empty for a local model)' },
  'cls.test': { zh: '测试连接', en: 'Test connection' },
  'cls.testing': { zh: '测试中…', en: 'Testing…' },

  // ---- bilingual translation settings ----
  'trans.section': { zh: '双语翻译', en: 'Bilingual Translation' },
  'trans.enabled': { zh: '启用自动翻译', en: 'Enable automatic translation' },
  'trans.hint': {
    zh: '优先使用免费的本地大模型（Ollama / LM Studio，无需 API Key，数据不出本机）；只有本地不可用时才建议改用商用 API。只翻译标题与摘要，不抓取正文。',
    en: 'Free local models come first (Ollama / LM Studio — no API key, nothing leaves this machine). Use a commercial API only when no local model is available. Only titles and abstracts are translated; full text is never fetched.',
  },
  'trans.modeLocal': { zh: '本地大模型（免费）', en: 'Local model (free)' },
  'trans.modeCloud': { zh: '商用 API（可能收费）', en: 'Commercial API (may cost money)' },
  'trans.modeOff': { zh: '关闭', en: 'Off' },
  'trans.engine': { zh: '翻译引擎', en: 'Translation engine' },
  'trans.engineLLM': { zh: 'LLM', en: 'LLM' },
  'trans.engineMTran': { zh: 'MTranServer（本地免费）', en: 'MTranServer (local, free)' },
  'trans.engineLibre': { zh: 'LibreTranslate（本地免费）', en: 'LibreTranslate (local, free)' },
  'trans.mtranUrl': { zh: 'MTranServer 地址', en: 'MTranServer URL' },
  'trans.mtranToken': { zh: 'MTranServer Token（默认留空）', en: 'MTranServer token (leave empty by default)' },
  'trans.mtranHint': {
    zh: '默认 http://localhost:8989。纯本地运行，首次翻译自动下载语言模型。',
    en: 'Default http://localhost:8989. Fully local; the language model downloads on first use.',
  },
  'trans.libreUrl': { zh: 'LibreTranslate 地址', en: 'LibreTranslate URL' },
  'trans.libreKey': { zh: 'LibreTranslate API Key（可留空）', en: 'LibreTranslate API key (optional)' },
  'trans.libreHint': { zh: '默认 http://localhost:5000', en: 'Default http://localhost:5000' },
  'trans.detect': { zh: '检测本地模型', en: 'Detect local models' },
  'trans.detecting': { zh: '检测中…', en: 'Detecting…' },
  'trans.detected': { zh: '检测到的本地模型服务', en: 'Detected local model servers' },
  'trans.noneDetected': {
    zh: '未检测到本地模型服务。请先安装并启动 Ollama（默认端口 11434），然后点击“检测本地模型”。',
    en: 'No local model server found. Install and start Ollama (default port 11434), then click "Detect local models".',
  },
  'trans.baseUrl': { zh: '接口地址', en: 'Base URL' },
  'trans.model': { zh: '模型名称', en: 'Model' },
  'trans.apiKey': { zh: 'API Key（本地模型留空）', en: 'API key (leave empty for a local model)' },
  'trans.scope': { zh: '翻译范围', en: 'Translate scope' },
  'trans.scopeTitle': { zh: '仅标题（最快）', en: 'Title only (fastest)' },
  'trans.scopeTitleSummary': { zh: '标题 + 摘要（推荐）', en: 'Title + abstract (recommended)' },
  'trans.scopeAll': { zh: '标题 + 摘要 + 导语', en: 'Title + abstract + lead' },
  'trans.batchSize': { zh: '每批文章数', en: 'Articles per batch' },
  'trans.maxConcurrency': { zh: 'LLM 并发数', en: 'LLM concurrency' },
  'trans.maxConcurrencyHint': { zh: '单个任务内同时进行的 LLM 请求数（1-8），本地模型请按其承受能力设置', en: 'Parallel LLM requests per job (1-8); size it to what your local model can take' },
  'trans.timeout': { zh: '单次请求超时（秒）', en: 'Request timeout (seconds)' },
  'trans.dailyLimit': { zh: '每日翻译上限（0 = 不限）', en: 'Daily limit (0 = unlimited)' },
  'trans.test': { zh: '测试连接', en: 'Test connection' },
  'trans.testing': { zh: '测试中…', en: 'Testing…' },
  'trans.runNow': { zh: '立即翻译待处理文章', en: 'Translate pending now' },
  'trans.running': { zh: '翻译中…', en: 'Running…' },
  'trans.resetFailed': { zh: '重置失败项并重试', en: 'Reset failed and retry' },
  'trans.status': { zh: '翻译状态', en: 'Translation status' },
  'trans.reachable': { zh: '接口可用', en: 'Endpoint reachable' },
  'trans.unreachable': { zh: '接口不可用', en: 'Endpoint unreachable' },
  'trans.configured': { zh: '已配置', en: 'Configured' },
  'trans.notConfigured': { zh: '未配置', en: 'Not configured' },
  'trans.statTotal': { zh: '文章总数', en: 'Articles' },
  'trans.statDone': { zh: '双语完成', en: 'Bilingual done' },
  'trans.statPending': { zh: '待翻译', en: 'Pending' },
  'trans.statFailed': { zh: '失败', en: 'Failed' },
  'trans.statSkipped': { zh: '跳过', en: 'Skipped' },
  'trans.statCache': { zh: '缓存条目', en: 'Cached translations' },
  'trans.translatedToday': { zh: '今日已翻译', en: 'Translated today' },
  'trans.lastRun': { zh: '上次运行', en: 'Last run' },
  'trans.auditTitle': { zh: '分类审计', en: 'Classification audit' },
  'trans.auditHint': { zh: '检查每篇文章的分类是否合理。仅命中一个泛化关键词的文章会被标记为疑似误分类。', en: 'Check whether article classifications are reasonable. Articles that matched on a single weak keyword are flagged as suspect.' },
  'trans.runAudit': { zh: '运行审计', en: 'Run audit' },
  'trans.auditing': { zh: '审计中…', en: 'Auditing…' },
  'trans.auditCategories': { zh: '各分类统计', en: 'Per-category statistics' },
  'trans.auditCategory': { zh: '分类', en: 'Category' },
  'trans.auditTotal': { zh: '文章数', en: 'Articles' },
  'trans.auditWeak': { zh: '疑似误分类', en: 'Suspect' },
  'trans.auditSuspects': { zh: '疑似误分类文章', en: 'Suspect articles' },
  'trans.auditKeyword': { zh: '命中关键词', en: 'Matched keyword' },
  'trans.auditClean': { zh: '未发现疑似误分类文章。', en: 'No suspect articles found.' },
  'trans.auditDone': { zh: '审计完成', en: 'Audit finished' },
  'trans.classifyUnclassified': { zh: '分类未分类文章', en: 'Classify unclassified' },
  'trans.classifying': { zh: '分类中…', en: 'Classifying…' },
  'trans.reclassifySuspects': { zh: '重新分类疑似文章', en: 'Re-classify suspects' },
  'trans.reclassifying': { zh: '重新分类中…', en: 'Re-classifying…' },
  'trans.jobStarted': { zh: '任务已加入队列，将按顺序执行。', en: 'Job queued; it runs in order.' },
  'trans.jobRunning': { zh: '同类任务已在队列中。', en: 'A job of this kind is already queued.' },
  'trans.cancelTranslate': { zh: '取消翻译', en: 'Cancel translation' },
  'trans.cancelClassify': { zh: '取消分类', en: 'Cancel classification' },
  'trans.cancelAll': { zh: '全部取消', en: 'Cancel all' },
  'trans.cancelled': { zh: '已发送取消请求。', en: 'Cancellation requested.' },
  'trans.queueLabel': { zh: '队列', en: 'Queue' },
  'trans.currentJob': { zh: '当前任务', en: 'Current job' },
  'trans.jobBatch': { zh: '批量翻译', en: 'Batch translation' },
  'trans.jobSingle': { zh: '单篇翻译', en: 'Single-article translation' },
  'card.llmClassified': { zh: '由 AI 分类', en: 'Classified by AI' },
  'tasks.title': { zh: '任务监控', en: 'Task Monitor' },
  'tasks.current': { zh: '当前任务', en: 'Current task' },
  'tasks.queued': { zh: '排队', en: 'Queued' },
  'tasks.running': { zh: '已运行', en: 'Running for' },
  'tasks.idle': { zh: '暂无执行中的任务', en: 'No task is running' },
  'tasks.empty': { zh: '暂无事件记录', en: 'No events yet' },
  'tasks.clear': { zh: '清空记录', en: 'Clear' },
  'tasks.feed': { zh: '任务事件', en: 'Task events' },
  'tasks.cancelJob': { zh: '取消该任务', en: 'Cancel this task' },
  'tasks.watch': { zh: '监控', en: 'Monitor' },
  'tasks.evtQueued': { zh: '已排队', en: 'Queued' },
  'tasks.evtStart': { zh: '开始', en: 'Started' },
  'tasks.evtDone': { zh: '完成', en: 'Completed' },
  'tasks.evtError': { zh: '失败', en: 'Failed' },
  'tasks.evtCancelled': { zh: '已取消', en: 'Cancelled' },
  'tasks.evtClassifying': { zh: '正在分类', en: 'Classifying' },
  'tasks.evtClassified': { zh: '分类', en: 'Classified' },
  'tasks.evtTranslating': { zh: '正在翻译', en: 'Translating' },
  'tasks.evtTranslated': { zh: '翻译', en: 'Translated' },
  'tasks.evtUnmatched': { zh: '未匹配到分类', en: 'no category matched' },
  'tasks.evtSkipTranslate': { zh: '无需翻译', en: 'nothing to translate' },
  'tasks.evtSummary': { zh: '共处理', en: 'Processed' },
  'tasks.unitArticles': { zh: '篇', en: 'articles' },
};

@Injectable({ providedIn: 'root' })
export class I18nService {
  private readonly langSignal = signal<Lang>(readStoredLang());
  private loaded = false;

  readonly lang = this.langSignal.asReadonly();
  readonly isZh = computed(() => this.langSignal() === 'zh');

  /**
   * Applies the language stored in the backend settings.
   *
   * localStorage alone is too fragile: it disappears with the WebView2 profile,
   * and a fresh install silently falls back to Chinese even for a user who chose
   * English. Called once at startup; a failure is not fatal, the cached value
   * simply stays in effect.
   */
  async load(): Promise<void> {
    if (this.loaded) return;
    this.loaded = true;
    try {
      const { SettingGetAll } = await import('../../../wailsjs/go/bindings/Bindings');
      const all = await SettingGetAll();
      const stored = (all?.['ui_language'] || '').trim();
      if (stored === 'zh' || stored === 'en') {
        this.langSignal.set(stored);
        persist(stored);
      }
    } catch (err) {
      console.debug('[i18n] could not read stored language, using the cached one:', err);
    }
  }

  setLang(lang: Lang): void {
    if (lang !== 'zh' && lang !== 'en') return;
    this.langSignal.set(lang);
    persist(lang);
    // Persist to the backend too (fire and forget): the UI must not wait on it.
    void this.persistRemote(lang);
  }

  private async persistRemote(lang: Lang): Promise<void> {
    try {
      const { SettingSet } = await import('../../../wailsjs/go/bindings/Bindings');
      await SettingSet('ui_language', lang);
    } catch (err) {
      console.debug('[i18n] could not persist the language:', err);
    }
  }

  toggle(): void {
    this.setLang(this.langSignal() === 'zh' ? 'en' : 'zh');
  }

  /** Translates a key. Unknown keys return the key itself so a typo is visible. */
  t(key: string, params?: Record<string, string | number>): string {
    const entry = DICT[key];
    let text = entry ? entry[this.langSignal()] : key;
    if (params) {
      for (const [k, v] of Object.entries(params)) {
        text = text.split(`{{${k}}}`).join(String(v));
      }
    }
    return text;
  }

  /** t() bound to a value that changes with the language (for computed/template use). */
  translate(key: string, params?: Record<string, string | number>): string {
    return this.t(key, params);
  }

  /** All keys currently defined, used by tests. */
  static keys(): string[] {
    return Object.keys(DICT);
  }
}

function persist(lang: Lang): void {
  try {
    localStorage.setItem(STORAGE_KEY, lang);
  } catch {
    // Private mode / disabled storage: the language still applies for this
    // session, it just will not be remembered locally.
  }
}

function readStoredLang(): Lang {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw === 'en' || raw === 'zh') return raw;
  } catch {
    // ignore
  }
  // Default to Chinese: the seeded content sources and the category tree are
  // primarily Chinese, so that is the language most users start in.
  return 'zh';
}
