package repository

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"ai-informations/internal/langdetect"
	"ai-informations/internal/models"

	"gorm.io/gorm"
)

// AutoMigrate runs all schema migrations.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&models.Source{},
		&models.SourceFilterRule{},
		&models.Article{},
		&models.Category{},
		&models.ArticleCategory{},
		&models.Tag{},
		&models.ArticleTag{},
		&models.Follow{},
		&models.UserBehavior{},
		&models.UserProfile{},
		&models.HotRank{},
		&models.FetchLog{},
		&models.FilterLog{},
		&models.SystemSetting{},
		&models.Translation{},
	)
}

// SeedDefaultCategories inserts the default category tree from requirements.
//
// Every category carries a Chinese and an English name/description so the UI can
// switch language without a translation round-trip. Existing rows are updated in
// place when the English side is still empty, which is what upgrades a database
// created before the bilingual columns existed.
func SeedDefaultCategories(db *gorm.DB) error {
	type seed struct{ slug, name, nameEn, desc, descEn string }

	seeds := []seed{
		// Finance (财经)
		{"finance-macro", "宏观经济", "Macro Economy", "国内生产总值、CPI、PPI、失业率、PMI、进出口", "GDP, CPI, PPI, unemployment, PMI, trade"},
		{"finance-securities", "证券市场", "Securities Market", "全球股指、上市公司、券商与机构观点", "Stock indices, listed companies, broker views"},
		{"finance-fixed-income-forex", "固收与外汇", "Fixed Income & FX", "债券、国债收益率、主要货币汇率", "Bonds, treasury yields, forex rates"},
		{"finance-commodities", "大宗商品", "Commodities", "能源、金属、农产品等商品行情", "Energy, metals, agriculture commodities"},
		{"finance-industry", "行业产业资讯", "Industry News", "新能源、半导体、地产、医药等产业链动态", "New energy, semiconductors, real estate, pharma"},
		{"finance-banking-insurance", "银行保险资管", "Banking, Insurance & Asset Management", "银行存款利率、保险偿付、基金资管", "Bank rates, insurance, fund management"},
		{"finance-intl-trade", "国际贸易", "International Trade", "进出口数据、关税、贸易协定与摩擦", "Import/export, tariffs, trade agreements"},
		{"finance-commentary", "财经评论", "Finance Commentary", "专家观点、投行研报摘要、重大经济事件", "Expert views, research reports, economic events"},
		{"finance-regulation", "财税金融监管", "Fiscal & Financial Regulation", "监管新规、反垄断、金融科技政策", "Regulatory updates, anti-monopoly, fintech"},
		// Military (军事)
		{"military-defense-policy", "防务政策与战略", "Defense Policy & Strategy", "国防预算、国防白皮书、国家安全战略", "Defense budgets, white papers, military strategy"},
		{"military-equipment", "武器装备与技术", "Weapons & Technology", "新型武器研发列装、军工技术与军售", "New weapons, military technology, arms exports"},
		{"military-exercises", "军演与军事部署", "Exercises & Deployments", "联合军演、兵力调动、海外基地变化", "Joint exercises, troop deployments, base changes"},
		{"military-conflicts", "地区冲突与战场", "Conflicts & Battlefields", "局部战事进展、停火谈判、边境摩擦", "Local conflicts, ceasefire, border tensions"},
		{"military-personnel", "军队人事与高层", "Military Personnel & Leadership", "国防部长与军队高层任免、军方表态", "Defense minister appointments, military leadership"},
		{"military-arm-control", "军控与核相关", "Arms Control & Nuclear", "核力量动态、核试验、军控与裁军条约", "Nuclear tests, arms control treaties"},
		{"military-security", "军事安全与反恐", "Security & Counter-Terrorism", "反恐行动、反海盗、航道与空域安全", "Counter-terrorism, anti-piracy, maritime security"},
		{"military-history", "军事历史与智库", "Military History & Think Tanks", "战史复盘、防务智库报告、军力评估", "Military history, defense think tank reports"},
		{"military-disaster", "军事灾害与救援", "Disaster Relief Operations", "军队参与救灾、海外撤侨、人道救援", "Military disaster relief, evacuation operations"},
		// Technology (科技)
		{"tech-science", "基础科学研究", "Basic Science", "天文航天、物理化学生物等基础研究突破", "Astronomy, physics, biology, new discoveries"},
		{"tech-it-ai", "信息技术与AI", "IT & AI", "AI 大模型、芯片半导体、软件与网络安全", "AI models, chips, software, cybersecurity"},
		{"tech-hardware", "硬件与消费电子", "Hardware & Consumer Electronics", "手机电脑、机器人、无人机、VR/AR、可穿戴", "Phones, PCs, robots, VR/AR, wearables"},
		{"tech-biomed", "生物医药与生命科学", "Biomedicine & Life Sciences", "新药研发、临床试验、基因与细胞治疗", "Drug development, gene therapy, medical devices"},
		{"tech-green-energy", "新能源与绿色科技", "New Energy & Green Tech", "光伏、风电、储能、氢能、电动汽车", "Solar, wind, storage, hydrogen, EV"},
		{"tech-manufacturing", "工业制造与工程", "Manufacturing & Engineering", "智能制造、工业机器人、3D 打印、重大工程", "Smart manufacturing, industrial robots, 3D printing"},
		{"tech-policy", "科技政策与监管", "Tech Policy & Regulation", "科技法案、技术出口管制、专利与知识产权", "Tech laws, export controls, patents"},
		{"tech-companies", "科技企业动态", "Tech Company News", "科技公司技术发布、研发投入、技术并购", "Tech company product launches, R&D"},
		{"tech-frontier", "前沿技术", "Frontier Technology", "量子计算、脑机接口、合成生物学", "Quantum computing, brain-computer interface"},
		{"tech-risks", "科技灾难与风险", "Tech Disasters & Risks", "技术事故、实验室安全、AI 风险与技术伦理", "Tech accidents, lab safety, AI risks"},
		// Politics (政治)
		{"politics-domestic", "各国国内政治", "Domestic Politics", "选举、政府人事、立法与政局动态", "Elections, government personnel, legislation"},
		{"politics-diplomacy", "外交与国际关系", "Diplomacy & International Relations", "元首会晤、多边会议、外交表态与制裁", "Summits, multilateral meetings, diplomatic statements"},
		{"politics-geopolitics", "地缘政治与地区局势", "Geopolitics & Regional Affairs", "地区冲突、领土争端、地区安全架构", "Regional conflicts, territorial disputes"},
		{"politics-governance", "国际治理与人权", "Global Governance & Human Rights", "国际公约、全球治理议题与规则修订", "International conventions, global governance"},
		{"politics-commentary", "政治评论与智库", "Political Commentary & Think Tanks", "各国智库对政局与地缘形势的分析预测", "Think tank analysis, political predictions"},
		// Economy (经济)
		{"economy-indicators", "宏观经济指标", "Macro Indicators", "GDP、CPI、PPI、失业率、PMI、工业增加值", "GDP, CPI, PPI, unemployment, PMI"},
		{"economy-monetary-fiscal", "货币与财政", "Monetary & Fiscal Policy", "央行利率、存款准备金、M2、财政预算与国债", "Central bank rates, fiscal policy, M2"},
		{"economy-regional-global", "区域与全球经济", "Regional & Global Economy", "各国经济展望、IMF 与世界银行报告", "Economic outlook, IMF/World Bank reports"},
		{"economy-industry", "产业经济", "Industrial Economy", "行业供需、产能变化、产业扶持与限制政策", "Industry supply-demand, industrial policy"},
		{"economy-demographics", "人口与民生", "Demographics & Livelihood", "人口数据、居民收入、储蓄与住房市场", "Population data, income, housing"},
		{"economy-policy", "经济政策与改革", "Economic Policy & Reform", "经济改革方案、反垄断、营商环境", "Economic reform, anti-monopoly, business environment"},
		{"economy-research", "经济研究与风险", "Economic Research & Risk", "经济预测报告、债务与通胀风险预警", "Economic forecasts, risk warnings"},
	}

	for i := range seeds {
		s := seeds[i]
		var existing models.Category
		err := db.Where("slug = ?", s.slug).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := db.Create(&models.Category{
				Name: s.name, NameEn: s.nameEn,
				Slug:        s.slug,
				Description: s.desc, DescriptionEn: s.descEn,
			}).Error; err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}

		updates := map[string]interface{}{}
		// Upgrade path: only fill the blank English side, never rewrite names the
		// user may have edited by hand.
		if strings.TrimSpace(existing.NameEn) == "" {
			updates["name_en"] = s.nameEn
		}
		if strings.TrimSpace(existing.DescriptionEn) == "" {
			updates["description_en"] = s.descEn
		}
		// The original seed put the English text in the Chinese description
		// column, so a Chinese UI showed English descriptions. Replace it only
		// when it is still exactly the seeded English default, which means the
		// user never edited it.
		if strings.TrimSpace(existing.Description) == "" || strings.TrimSpace(existing.Description) == s.descEn {
			updates["description"] = s.desc
		}
		if len(updates) > 0 {
			if err := db.Model(&models.Category{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// SeedDefaultSettings inserts default system settings.
func SeedDefaultSettings(db *gorm.DB) error {
	settings := []models.SystemSetting{
		{Key: "max_concurrency", Value: "3", Description: "Global max collector goroutines"},
		{Key: "default_interval_seconds", Value: "300", Description: "Default interval between requests per source"},
		{Key: "article_ttl_days", Value: "90", Description: "Days to keep articles before auto-deletion"},
		{Key: "log_level", Value: "info", Description: "Logging level: debug, info, warn, error"},
		{Key: "compliance_filter_enabled", Value: "true", Description: "Enable compliance content filter"},
		{Key: "robots_enforcement", Value: "true", Description: "Enforce robots.txt for all sources"},
		{Key: "auto_collect", Value: "true", Description: "Auto-start collector on application startup"},
		{Key: "ui_language", Value: "zh", Description: "UI language: zh | en"},
		// Bilingual content generation.
		//
		// Free first: the default provider is a LOCAL model on 127.0.0.1, which
		// costs nothing, needs no API key, and keeps every headline on this
		// machine (see 需求.md rule 8 - nothing may be uploaded). A commercial
		// API is opt-in, and the daily limit caps what it can spend.
		{Key: "translate_enabled", Value: "false", Description: "Enable automatic article translation"},
		{Key: "translate_provider", Value: "local", Description: "local (free, Ollama/LM Studio) | cloud (commercial API) | off"},
		{Key: "llm_base_url", Value: "http://127.0.0.1:11434/v1", Description: "OpenAI-compatible API base URL"},
		{Key: "llm_model", Value: "qwen2.5:7b", Description: "Model name used for translation"},
		{Key: "llm_api_key", Value: "", Description: "API key for the translation provider (empty for local models)"},
		{Key: "translate_scope", Value: "title_summary", Description: "title | title_summary | all"},
		{Key: "translate_batch_size", Value: "10", Description: "Articles per translation batch"},
		{Key: "llm_max_concurrency", Value: "1", Description: "Parallel LLM requests per job (1-8)"},
		{Key: "translate_timeout_seconds", Value: "120", Description: "Per-request timeout for the translation provider"},
		{Key: "translate_daily_limit", Value: "300", Description: "Max articles translated per day (cost guard, 0 = unlimited)"},
		{Key: "translate_engine", Value: "llm", Description: "Translation engine: llm | mtranserver | libretranslate"},
		{Key: "mtranserver_url", Value: "http://localhost:8989", Description: "MTranServer base URL (engine: mtranserver)"},
		{Key: "mtranserver_token", Value: "", Description: "MTranServer API token (empty when the server has no MT_API_TOKEN)"},
		{Key: "libretranslate_url", Value: "http://localhost:5000", Description: "LibreTranslate base URL (engine: libretranslate)"},
		{Key: "libretranslate_api_key", Value: "", Description: "LibreTranslate API key (empty when the deployment is open)"},
		// Narration audio for newly fetched articles (edge-tts, offline-independent
		// of the local LLM: the Microsoft endpoint only takes text in, never the
		// reverse — nothing from the library is uploaded).
		{Key: "tts_enabled", Value: "true", Description: "Synthesise narration audio after each collection round"},
		{Key: "tts_engine", Value: "edge", Description: "TTS engine: edge (free edge-tts CLI) | api (OpenAI-compatible TTS API)"},
		{Key: "tts_voice", Value: "zh-CN-XiaoxiaoNeural", Description: "edge-tts voice name"},
		{Key: "tts_max_per_round", Value: "50", Description: "Max articles synthesised per round (0 = unlimited)"},
		{Key: "tts_concurrency", Value: "3", Description: "Parallel synthesis workers per round (1-8, one hidden python process each)"},
		{Key: "tts_auto_play", Value: "true", Description: "Auto-play the round playlist on hot rank / recommendation pages"},
		{Key: "tts_python", Value: "", Description: "Python executable with edge-tts installed (empty = auto-detect)"},
		{Key: "tts_api_url", Value: "", Description: "TTS API base ending in /v1 (engine: api); requests go to <url>/audio/speech"},
		{Key: "tts_api_key", Value: "", Description: "TTS API bearer token (engine: api)"},
		{Key: "tts_api_model", Value: "", Description: "TTS API model name (engine: api)"},
		{Key: "tts_api_voice", Value: "", Description: "TTS API voice name (engine: api, provider-specific)"},
	}
	// Classification used to share the translation model (llm_*). Give it its
	// own keys that inherit the CURRENT llm_* values exactly once, so behaviour
	// does not change at upgrade time and the two never move together again
	// (these rows are user-configurable: insert-if-not-exists below).
	inherit := func(key, def string) string {
		var vals []string
		if err := db.Model(&models.SystemSetting{}).Where("key = ?", key).Pluck("value", &vals).Error; err == nil && len(vals) > 0 {
			if v := strings.TrimSpace(vals[0]); v != "" {
				return v
			}
		}
		return def
	}
	settings = append(settings,
		models.SystemSetting{Key: "cls_base_url", Value: inherit("llm_base_url", "http://127.0.0.1:11434/v1"), Description: "Classifier chat model base URL (independent from translation)"},
		models.SystemSetting{Key: "cls_model", Value: inherit("llm_model", "qwen2.5:7b"), Description: "Model name used for classification"},
		models.SystemSetting{Key: "cls_api_key", Value: inherit("llm_api_key", ""), Description: "API key for the classifier model (empty for local models)"},
	)
	// Settings that are system-level defaults (not user-tuned) should always
	// be enforced on startup so stale values (e.g. an old "30" interval) get
	// corrected. User-configurable settings below keep the original
	// "insert-if-not-exists" behaviour so existing choices are preserved.
	sysKeys := map[string]bool{
		"max_concurrency":           true,
		"default_interval_seconds":  true,
		"article_ttl_days":          true,
		"log_level":                 true,
		"compliance_filter_enabled": true,
		"robots_enforcement":        true,
		"auto_collect":              true,
		"ui_language":               true,
	}
	for _, s := range settings {
		if sysKeys[s.Key] {
			// Upsert: always enforce the current default.
			db.Where("key = ?", s.Key).Assign(s).FirstOrCreate(&models.SystemSetting{})
		} else {
			// User-configurable: only insert when the key is missing.
			var count int64
			db.Model(&models.SystemSetting{}).Where("key = ?", s.Key).Count(&count)
			if count == 0 {
				db.Create(&s)
			}
		}
	}
	// The auto-run interval died with the queue rewrite; purge the orphaned
	// key from older databases. Hard delete: gorm.Model's soft delete would
	// leave the row (and its unique index) behind forever.
	db.Unscoped().Where("key = ?", "translate_interval_seconds").Delete(&models.SystemSetting{})
	return nil
}

// SeedDefaultSources inserts default data sources verified accessible from China.
func SeedDefaultSources(db *gorm.DB) error {
	sources := []models.Source{
		// 中国新闻 - 国内可直接访问
		{Name: "中国新闻网-国际", Type: "rss", URL: "http://www.chinanews.com.cn/rss/world.xml", CronExpression: "0 */4 * * *", RequestInterval: 60, IsActive: true, Status: "unknown"},
		{Name: "中国新闻网-政治", Type: "rss", URL: "http://www.chinanews.com.cn/rss/politics.xml", CronExpression: "0 */6 * * *", RequestInterval: 90, IsActive: true, Status: "unknown"},
		{Name: "中国新闻网-社会", Type: "rss", URL: "http://www.chinanews.com.cn/rss/society.xml", CronExpression: "0 */6 * * *", RequestInterval: 90, IsActive: true, Status: "unknown"},
		{Name: "中国新闻网-文化", Type: "rss", URL: "http://www.chinanews.com.cn/rss/culture.xml", CronExpression: "0 */8 * * *", RequestInterval: 120, IsActive: true, Status: "unknown"},
		{Name: "中国新闻网-体育", Type: "rss", URL: "http://www.chinanews.com.cn/rss/sports.xml", CronExpression: "0 */8 * * *", RequestInterval: 120, IsActive: false, Status: "unknown"},
		// 人民网 - 中央重点新闻网站
		{Name: "人民网-时政", Type: "rss", URL: "http://www.people.com.cn/rss/politics.xml", CronExpression: "0 */4 * * *", RequestInterval: 60, IsActive: true, Status: "unknown"},
		{Name: "人民网-军事", Type: "rss", URL: "http://www.people.com.cn/rss/military.xml", CronExpression: "0 */6 * * *", RequestInterval: 90, IsActive: true, Status: "unknown"},
		{Name: "人民网-全部新闻", Type: "rss", URL: "http://www.people.com.cn/rss/ywkx.xml", CronExpression: "0 */8 * * *", RequestInterval: 120, IsActive: true, Status: "unknown"},
		{Name: "人民网-台港澳", Type: "rss", URL: "http://www.people.com.cn/rss/haixia.xml", CronExpression: "0 */8 * * *", RequestInterval: 120, IsActive: true, Status: "unknown"},
		// 新华网 - 中央新闻单位
		{Name: "新华网-时政", Type: "rss", URL: "http://www.xinhuanet.com/politics/news_politics.xml", CronExpression: "0 */4 * * *", RequestInterval: 60, IsActive: true, Status: "unknown"},
		// 科技媒体
		{Name: "少数派", Type: "rss", URL: "https://sspai.com/feed", CronExpression: "0 */6 * * *", RequestInterval: 90, IsActive: true, Status: "unknown"},
		{Name: "IT之家", Type: "rss", URL: "https://www.ithome.com/rss/", CronExpression: "0 */4 * * *", RequestInterval: 60, IsActive: true, Status: "unknown"},
		{Name: "Solidot", Type: "rss", URL: "https://www.solidot.org/index.rss", CronExpression: "0 */6 * * *", RequestInterval: 90, IsActive: true, Status: "unknown"},
		{Name: "虎嗅", Type: "rss", URL: "https://rss.huxiu.com/", CronExpression: "0 */6 * * *", RequestInterval: 90, IsActive: true, Status: "unknown"},
		// 经济数据
		{Name: "国家统计局", Type: "rss", URL: "http://www.stats.gov.cn/english/PressRelease/rss.xml", CronExpression: "0 */8 * * *", RequestInterval: 120, IsActive: true, Status: "unknown"},
		// International - verified accessible from China
		{Name: "Defense News", Type: "rss", URL: "https://www.defensenews.com/arc/outboundfeeds/rss/", CronExpression: "0 */6 * * *", RequestInterval: 60, IsActive: true, Status: "unknown"},
		{Name: "Fox News", Type: "rss", URL: "https://feeds.foxnews.com/foxnews/latest", CronExpression: "0 */6 * * *", RequestInterval: 60, IsActive: true, Status: "unknown"},
		{Name: "Hacker News", Type: "rss", URL: "https://hnrss.org/frontpage", CronExpression: "0 */4 * * *", RequestInterval: 45, IsActive: true, Status: "unknown"},
		// Financial news via local China Finance RSS Bridge (localhost:8053,
		// tools/china-finance-rss in this repo). The bridge must be running —
		// start it with tools\china-finance-rss\start-bridge.bat.
		{Name: "财联社电报", Type: "rss", URL: "http://localhost:8053/cls/telegraph", CronExpression: "0 */2 * * *", RequestInterval: 30, IsActive: true, Status: "unknown", Publisher: "财联社"},
		// Sina finance: served by the local News RSS Bridge (localhost:1200,
		// tools/news-rss-bridge, Python — no Docker required).
		// The bare /sina/finance route was removed upstream; use /rollnews.
		{Name: "新浪财经-财经", Type: "rss", URL: "http://localhost:1200/sina/finance/rollnews", CronExpression: "0 */4 * * *", RequestInterval: 60, IsActive: true, Status: "unknown", Publisher: "新浪财经"},
		{Name: "华尔街见闻-实时快讯", Type: "rss", URL: "http://localhost:8053/wallstreetcn/live", CronExpression: "0 */2 * * *", RequestInterval: 30, IsActive: true, Status: "unknown", Publisher: "华尔街见闻"},
		{Name: "金十数据-快讯", Type: "rss", URL: "http://localhost:8053/jin10/flash", CronExpression: "0 */2 * * *", RequestInterval: 30, IsActive: true, Status: "unknown", Publisher: "金十数据"},
		{Name: "东方财富-快讯", Type: "rss", URL: "http://localhost:8053/eastmoney/kuaixun", CronExpression: "0 */4 * * *", RequestInterval: 60, IsActive: true, Status: "unknown", Publisher: "东方财富"},
		{Name: "同花顺-快讯", Type: "rss", URL: "http://localhost:8053/ths/kuaixun", CronExpression: "0 */4 * * *", RequestInterval: 60, IsActive: true, Status: "unknown", Publisher: "同花顺"},
		// East Money strategy research via the local News RSS Bridge
		// (localhost:1200). Remains disabled by default — flip IsActive to
		// opt in (the old RSSHub image bug no longer applies).
		{Name: "东方财富-策略研报", Type: "rss", URL: "http://localhost:1200/eastmoney/report/strategyreport", CronExpression: "0 */6 * * *", RequestInterval: 90, IsActive: false, Status: "unknown", Publisher: "东方财富"},
		// ForgeRSS hosts a reliable jsDelivr CDN feed for CNInfo announcements.
		// Updated every 6 hours by CI; no rate-limit risk compared to RSSHub.
		{Name: "巨潮资讯-公司公告", Type: "rss", URL: "https://cdn.jsdelivr.net/gh/tmwgsicp/ForgeRSS@main/feeds/feed_cninfo_announcements.xml", CronExpression: "0 */6 * * *", RequestInterval: 90, IsActive: true, Status: "unknown", Publisher: "巨潮资讯网"},
		{Name: "巨潮资讯-雪球用户", Type: "rss", URL: "https://cdn.jsdelivr.net/gh/tmwgsicp/ForgeRSS@main/feeds/feed_xueqiu_user.xml", CronExpression: "0 */6 * * *", RequestInterval: 90, IsActive: true, Status: "unknown", Publisher: "雪球"},
		// Local News RSS Bridge (localhost:1200) feeds — verified 200 with items.
		{Name: "知乎日报", Type: "rss", URL: "http://localhost:1200/zhihu/daily", CronExpression: "0 */4 * * *", RequestInterval: 60, IsActive: true, Status: "unknown", Publisher: "知乎"},
		{Name: "知乎周刊", Type: "rss", URL: "http://localhost:1200/zhihu/weekly", CronExpression: "0 */8 * * *", RequestInterval: 120, IsActive: true, Status: "unknown", Publisher: "知乎"},
		{Name: "第一财经-直播", Type: "rss", URL: "http://localhost:1200/yicai/brief", CronExpression: "0 */2 * * *", RequestInterval: 30, IsActive: true, Status: "unknown", Publisher: "第一财经"},
		{Name: "财新网-最新文章", Type: "rss", URL: "http://localhost:1200/caixin/latest", CronExpression: "0 */4 * * *", RequestInterval: 60, IsActive: true, Status: "unknown", Publisher: "财新网"},
		{Name: "经济观察网-大宗商品", Type: "rss", URL: "http://localhost:1200/eeo/yaowen/dashi", CronExpression: "0 */6 * * *", RequestInterval: 90, IsActive: true, Status: "unknown", Publisher: "经济观察网"},
		{Name: "e公司快讯", Type: "rss", URL: "http://localhost:1200/egsea/flash", CronExpression: "0 */4 * * *", RequestInterval: 60, IsActive: true, Status: "unknown", Publisher: "e公司"},
		{Name: "格隆汇-快讯", Type: "rss", URL: "http://localhost:1200/gelonghui/live", CronExpression: "0 */4 * * *", RequestInterval: 60, IsActive: true, Status: "unknown", Publisher: "格隆汇"},
		{Name: "联合早报-中国", Type: "rss", URL: "http://localhost:1200/zaobao/realtime/china", CronExpression: "0 */4 * * *", RequestInterval: 60, IsActive: true, Status: "unknown", Publisher: "联合早报"},
		// RSSBridge note: the public rsshub.app instance is unreachable from
		// mainland China, and the former Docker RSSHub on localhost:1200 has
		// been replaced by a local Python service (no Docker dependency):
		//   project:  tools\news-rss-bridge (in this repo)
		//   start:    tools\news-rss-bridge\start-bridge.bat
		//   stop:     tools\news-rss-bridge\stop-bridge.bat
		//   health:   http://localhost:1200/healthz?check=1
		// Route paths stay RSSHub-compatible, so the source URLs below (and
		// any rows already stored in the DB) keep working unchanged. The
		// rsshub_instance system setting still points WebRSSAdapter there.
		// Routes served by the bridge:
		//   /yicai/brief                第一财经直播区
		//   /zhihu/daily                知乎日报
		//   /zhihu/weekly               知乎周刊 (已停刊 — 归档说明 feed)
		//   /caixin/latest              财新网最新文章
		//   /sina/finance/rollnews      新浪财经滚动 (bare /sina/finance was removed)
		//   /eeo/yaowen/dashi           经济观察网-大宗商品 (现行「快讯>商品」栏目)
		//   /egsea/flash                e公司快讯
		//   /gelonghui/live             格隆汇快讯
		//   /zaobao/realtime/china      联合早报-中港台即时
		//   /eastmoney/report/strategyreport  东方财富策略研报
		// 8053 routes (财联社/华尔街见闻/金十/东方财富快讯/同花顺) remain on
		// the China Finance RSS Bridge, also Docker-free.
	}

	for i := range sources {
		var count int64
		db.Model(&models.Source{}).Where("url = ?", sources[i].URL).Count(&count)
		if count == 0 {
			db.Create(&sources[i])
		}
	}
	return nil
}

// SourceRepository handles source CRUD and status operations.
type SourceRepository struct{ db *gorm.DB }

func NewSourceRepository(db *gorm.DB) *SourceRepository { return &SourceRepository{db: db} }

func (r *SourceRepository) List() ([]models.Source, error) {
	var sources []models.Source
	if err := r.db.Order("created_at DESC").Find(&sources).Error; err != nil {
		return nil, err
	}
	return sources, nil
}

func (r *SourceRepository) Get(id uint) (*models.Source, error) {
	var s models.Source
	if err := r.db.First(&s, id).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SourceRepository) Create(s *models.Source) error {
	// Defensive: reject blank sources at the repository layer to prevent
	// any code path from inserting a source with empty name/type/url.
	if strings.TrimSpace(s.Name) == "" || strings.TrimSpace(s.Type) == "" || strings.TrimSpace(s.URL) == "" {
		return fmt.Errorf("source name, type, and url are required")
	}
	return r.db.Create(s).Error
}

func (r *SourceRepository) Update(s *models.Source) error {
	// Update is a FULL-ROW write (gorm Save). It is only correct when the caller
	// passes a completely populated struct, normally obtained via Get().
	//
	// A partial struct such as &models.Source{Model: gorm.Model{ID: id}} makes
	// Save write zero values for every unset column, which silently blanked
	// name/type/url and reset created_at to 0001-01-01. That is what produced the
	// endless blank rows in `sources`. Reject it loudly instead of destroying data.
	if s.ID == 0 {
		return fmt.Errorf("source update requires a non-zero id")
	}
	if strings.TrimSpace(s.Name) == "" || strings.TrimSpace(s.Type) == "" || strings.TrimSpace(s.URL) == "" {
		return fmt.Errorf("source update requires name, type, and url; use TouchLastFetchedAt for partial updates")
	}
	return r.db.Save(s).Error
}

// TouchLastFetchedAt records a successful fetch without touching any other
// column. Use this instead of Update whenever only one field changes.
func (r *SourceRepository) TouchLastFetchedAt(id uint, t time.Time) error {
	if id == 0 {
		return fmt.Errorf("TouchLastFetchedAt requires a non-zero id")
	}
	return r.db.Model(&models.Source{}).Where("id = ?", id).
		Update("last_fetched_at", t).Error
}

// DeleteBlankSources removes sources that carry no name, type, or url. Such rows
// are unusable, and they used to be re-created on every startup because the
// seeder looks sources up by url (which had been blanked).
func (r *SourceRepository) DeleteBlankSources() (int64, error) {
	res := r.db.Where(
		"(name IS NULL OR TRIM(name) = '') AND (type IS NULL OR TRIM(type) = '') AND (url IS NULL OR TRIM(url) = '')",
	).Delete(&models.Source{})
	return res.RowsAffected, res.Error
}

func (r *SourceRepository) Delete(id uint) error {
	return r.db.Where("id = ?", id).Delete(&models.Source{}).Error
}

func (r *SourceRepository) Activate(id uint, active bool) error {
	return r.db.Model(&models.Source{}).Where("id = ?", id).Update("is_active", active).Error
}

func (r *SourceRepository) UpdateStatus(id uint, status, lastError string) error {
	return r.db.Model(&models.Source{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":     status,
		"last_error": lastError,
	}).Error
}

func (r *SourceRepository) IncrementFetchCount(id uint) error {
	return r.db.Model(&models.Source{}).Where("id = ?", id).Update("fetch_count", gorm.Expr("fetch_count + 1")).Error
}

// ArticleRepository handles article CRUD and queries.
type ArticleRepository struct{ db *gorm.DB }

func NewArticleRepository(db *gorm.DB) *ArticleRepository { return &ArticleRepository{db: db} }

// articleTimeExpr is the effective timestamp used for date filtering and for
// ordering: publish time when it is real, fetch time otherwise.
//
// publish_time cannot be compared as a raw string: RSS feeds hand back either
// "+00:00" or "+08:00" offsets (and sometimes microseconds), so the same instant
// sorts differently depending on its source. SQLite's datetime() normalizes
// both forms to UTC, which makes the comparison and the ordering correct. The
// 1900 guard skips Go's zero time, which is what an item without a date gets.
const articleTimeExpr = "CASE WHEN articles.publish_time IS NULL OR datetime(articles.publish_time) < '1900-01-01' " +
	"THEN datetime(articles.fetched_at) ELSE datetime(articles.publish_time) END"

// articleLocalDateExpr is the local calendar day of articleTimeExpr, so a
// "today" filter matches the user's wall clock rather than UTC.
const articleLocalDateExpr = "date(" + articleTimeExpr + ", 'localtime')"

// applyArticleFilter adds every NewsQuery condition to q. It is a package-level
// function because the hot-rank query joins the same articles table and must
// apply exactly the same filters.
func applyArticleFilter(q *gorm.DB, f models.NewsQuery) *gorm.DB {
	if f.SourceID != 0 {
		q = q.Where("articles.source_id = ?", f.SourceID)
	}
	if f.CategorySlug != "" {
		// A subquery rather than a JOIN: an article linked to two categories
		// would otherwise be returned (and counted) twice.
		q = q.Where("articles.id IN (SELECT ac.article_id FROM article_categories ac "+
			"JOIN categories c ON c.id = ac.category_id "+
			"WHERE c.slug = ? AND ac.deleted_at IS NULL AND c.deleted_at IS NULL)", f.CategorySlug)
	} else if f.CategoryPrefix != "" {
		// Module-wide view ("finance-"): every article in any sub-category.
		q = q.Where("articles.id IN (SELECT ac.article_id FROM article_categories ac "+
			"JOIN categories c ON c.id = ac.category_id "+
			"WHERE c.slug LIKE ? ESCAPE '\\' AND ac.deleted_at IS NULL AND c.deleted_at IS NULL)",
			escapeLike(f.CategoryPrefix)+"%")
	}
	if f.FollowOnly {
		q = q.Where("articles.id IN (SELECT f.target_id FROM follows f " +
			"WHERE f.follow_type = 'article' AND f.deleted_at IS NULL)")
	}
	if f.TitleKeyword != "" {
		// Match the title in any language so search works regardless of which
		// variant the user is reading.
		like := "%" + escapeLike(f.TitleKeyword) + "%"
		q = q.Where("(articles.title LIKE ? ESCAPE '\\' OR articles.title_zh LIKE ? ESCAPE '\\' OR articles.title_en LIKE ? ESCAPE '\\')",
			like, like, like)
	}
	if f.DateFrom != "" {
		q = q.Where(articleLocalDateExpr+" >= ?", f.DateFrom)
	}
	if f.DateTo != "" {
		q = q.Where(articleLocalDateExpr+" <= ?", f.DateTo)
	}
	return q
}

// escapeLike neutralizes LIKE wildcards so a user typing "%" searches for a
// literal percent sign instead of matching every row.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// orderArticles applies the requested sort. The effective timestamp is always
// the final tie-breaker, so the list is stable and strictly newest-first.
func orderArticles(q *gorm.DB, f models.NewsQuery) *gorm.DB {
	if f.SortBy == models.SortByHot {
		return q.Order("COALESCE(hot_ranks.score, 0) DESC").
			Order(articleTimeExpr + " DESC").
			Order("articles.id DESC")
	}
	return q.Order(articleTimeExpr + " DESC").Order("articles.id DESC")
}

// Query returns one page of articles matching the filter, plus the total number
// of matches. Every statement is built from a fresh *gorm.DB: GORM finishers
// (Count/Find) mutate the statement they run on, so reusing one builder for both
// the count and the page used to be a source of silently wrong results.
func (r *ArticleRepository) Query(f models.NewsQuery) ([]models.Article, int64, error) {
	f.Normalize()

	var total int64
	if err := applyArticleFilter(r.db.Model(&models.Article{}), f).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var articles []models.Article
	q := orderArticles(applyArticleFilter(r.db.Model(&models.Article{}), f), f)
	offset := (f.Page - 1) * f.PageSize
	if err := q.Preload("Categories").Offset(offset).Limit(f.PageSize).Find(&articles).Error; err != nil {
		return nil, 0, err
	}
	return articles, total, nil
}

// QueryLimited returns up to limit articles matching the filter in one page,
// for callers that rank or score in memory (recommendations).
func (r *ArticleRepository) QueryLimited(f models.NewsQuery, limit int) ([]models.Article, error) {
	f.Page = 1
	f.PageSize = limit
	articles, _, err := r.Query(f)
	return articles, err
}

// DistinctDates lists the local dates that actually contain articles, newest
// first, so the UI can offer real dates to pick from instead of guessing.
func (r *ArticleRepository) DistinctDates(limit int) ([]string, error) {
	if limit <= 0 || limit > 365 {
		limit = 60
	}
	var dates []string
	err := r.db.Raw("SELECT DISTINCT "+articleLocalDateExpr+" AS d FROM articles "+
		"WHERE articles.deleted_at IS NULL ORDER BY d DESC LIMIT ?", limit).Scan(&dates).Error
	if err != nil {
		return nil, err
	}
	// A row with no usable timestamp at all normalizes to NULL and would arrive
	// as an empty string; drop those so the picker never shows a blank option.
	out := make([]string, 0, len(dates))
	for _, d := range dates {
		if strings.TrimSpace(d) != "" {
			out = append(out, d)
		}
	}
	return out, nil
}

func (r *ArticleRepository) Get(id uint) (*models.Article, error) {
	var a models.Article
	if err := r.db.Preload("Categories").First(&a, id).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *ArticleRepository) Create(a *models.Article) error { return r.db.Create(a).Error }

func (r *ArticleRepository) BatchCreate(items []*models.Article) error {
	return r.db.Create(&items).Error
}

func (r *ArticleRepository) ExistsByFingerprint(fp string) (bool, error) {
	var count int64
	if err := r.db.Model(&models.Article{}).Where("fingerprint = ?", fp).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *ArticleRepository) ExistsByURL(url string) (bool, error) {
	var count int64
	if err := r.db.Model(&models.Article{}).Where("origin_url = ?", url).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *ArticleRepository) DeleteExpired(before time.Time) (int64, error) {
	result := r.db.Where("expire_at < ?", before).Delete(&models.Article{})
	return result.RowsAffected, result.Error
}

func (r *ArticleRepository) GetByFingerprints(fps []string) ([]uint, error) {
	var ids []uint
	r.db.Model(&models.Article{}).Select("id").Where("fingerprint IN ?", fps).Find(&ids)
	return ids, nil
}

// ---- Bilingual content -----------------------------------------------------

// NeedsTranslation reports whether an article is still missing the language
// variant opposite its original.
func NeedsTranslation(a *models.Article) bool {
	switch a.Lang {
	case langdetect.ZH:
		return strings.TrimSpace(a.TitleEn) == ""
	case langdetect.EN:
		return strings.TrimSpace(a.TitleZh) == ""
	default:
		return false // unknown original language: nothing sensible to translate into
	}
}

// ListPendingTranslation returns articles whose missing language variant still
// has to be produced. Failures are retried, but only up to maxAttempts times so
// a permanently broken provider cannot spin forever.
func (r *ArticleRepository) ListPendingTranslation(limit, maxAttempts int) ([]models.Article, error) {
	if limit <= 0 {
		limit = 10
	}
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	var articles []models.Article
	err := r.db.Where("trans_status IN ?", []string{"", models.TransPending, models.TransFailed}).
		Where("trans_attempts < ?", maxAttempts).
		Where("((lang = ? AND COALESCE(title_en, '') = '') OR (lang = ? AND COALESCE(title_zh, '') = ''))",
			langdetect.ZH, langdetect.EN).
		Order("fetched_at DESC").
		Limit(limit).
		Find(&articles).Error
	if err != nil {
		return nil, err
	}
	return articles, nil
}

// TranslationUpdate is a targeted write of the bilingual columns. A map is used
// deliberately: a struct-based Updates would skip empty strings, and clearing
// TransError on success is part of the contract.
type TranslationUpdate struct {
	TitleZh      string
	TitleEn      string
	SummaryZh    string
	SummaryEn    string
	LeadZh       string
	LeadEn       string
	Status       string
	Error        string
	Attempts     int
	TranslatedAt *time.Time
}

// ApplyTranslation writes the bilingual columns of one article without touching
// anything else.
func (r *ArticleRepository) ApplyTranslation(id uint, u TranslationUpdate) error {
	return r.db.Model(&models.Article{}).Where("id = ?", id).Updates(map[string]interface{}{
		"title_zh":       u.TitleZh,
		"title_en":       u.TitleEn,
		"summary_zh":     u.SummaryZh,
		"summary_en":     u.SummaryEn,
		"lead_zh":        u.LeadZh,
		"lead_en":        u.LeadEn,
		"trans_status":   u.Status,
		"trans_error":    u.Error,
		"trans_attempts": u.Attempts,
		"translated_at":  u.TranslatedAt,
	}).Error
}

// MarkTranslationAttempt increments the attempt counter alone, used when the
// provider call fails before any text is available.
func (r *ArticleRepository) MarkTranslationAttempt(id uint, status, errMsg string, attempts int) error {
	return r.db.Model(&models.Article{}).Where("id = ?", id).Updates(map[string]interface{}{
		"trans_status":   status,
		"trans_error":    truncateStr(errMsg, 500),
		"trans_attempts": attempts,
	}).Error
}

// BackfillBilingual upgrades rows written before the bilingual columns existed:
// it detects the original language and copies the original text into the
// matching side, then marks the row pending or done. It never overwrites the
// original columns and never invents a translation.
func BackfillBilingual(db *gorm.DB) (int, error) {
	var articles []models.Article
	if err := db.Where("lang = '' OR lang IS NULL OR (title_zh = '' AND title_en = '')").Find(&articles).Error; err != nil {
		return 0, err
	}

	updated := 0
	for i := range articles {
		a := &articles[i]
		lang := a.Lang
		if lang == "" {
			lang = langdetect.Detect(a.Title + " " + a.Summary)
		}
		if lang == langdetect.Unknown {
			continue
		}

		titleZh, titleEn := a.TitleZh, a.TitleEn
		summaryZh, summaryEn := a.SummaryZh, a.SummaryEn
		leadZh, leadEn := a.LeadZh, a.LeadEn

		if lang == langdetect.ZH {
			if titleZh == "" {
				titleZh = a.Title
			}
			if summaryZh == "" {
				summaryZh = a.Summary
			}
			if leadZh == "" {
				leadZh = a.Lead
			}
		} else {
			if titleEn == "" {
				titleEn = a.Title
			}
			if summaryEn == "" {
				summaryEn = a.Summary
			}
			if leadEn == "" {
				leadEn = a.Lead
			}
		}

		status := models.TransDone
		if (lang == langdetect.ZH && titleEn == "") || (lang == langdetect.EN && titleZh == "") {
			status = models.TransPending
		}

		if err := db.Model(&models.Article{}).Where("id = ?", a.ID).Updates(map[string]interface{}{
			"lang":         lang,
			"title_zh":     titleZh,
			"title_en":     titleEn,
			"summary_zh":   summaryZh,
			"summary_en":   summaryEn,
			"lead_zh":      leadZh,
			"lead_en":      leadEn,
			"trans_status": status,
		}).Error; err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}

// TranslationStats counts articles per translation status.
func (r *ArticleRepository) TranslationStats() (models.TranslationStats, error) {
	var s models.TranslationStats
	if err := r.db.Model(&models.Article{}).Count(&s.Total).Error; err != nil {
		return s, err
	}
	count := func(where string, args ...interface{}) (int64, error) {
		var n int64
		err := r.db.Model(&models.Article{}).Where(where, args...).Count(&n).Error
		return n, err
	}
	var err error
	if s.Done, err = count("trans_status = ?", models.TransDone); err != nil {
		return s, err
	}
	if s.Failed, err = count("trans_status = ?", models.TransFailed); err != nil {
		return s, err
	}
	if s.Skipped, err = count("trans_status = ?", models.TransSkipped); err != nil {
		return s, err
	}
	// "Pending" means "still missing a variant", which includes rows whose
	// status was never written because they predate the column.
	if s.Pending, err = count("((lang = ? AND COALESCE(title_en, '') = '') OR (lang = ? AND COALESCE(title_zh, '') = ''))",
		langdetect.ZH, langdetect.EN); err != nil {
		return s, err
	}
	// "Processable" is the subset that the worker will actually touch: within
	// retry limit so permanently-broken articles don't block the queue.
	if s.Processable, err = count(
		"(trans_status IN (?, ?, ?) AND trans_attempts < 3 AND ((lang = ? AND COALESCE(title_en, '') = '') OR (lang = ? AND COALESCE(title_zh, '') = '')))",
		"", models.TransPending, models.TransFailed,
		langdetect.ZH, langdetect.EN); err != nil {
		return s, err
	}
	return s, nil
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// TranslationRepository is the persistent cache of provider results.
type TranslationRepository struct{ db *gorm.DB }

func NewTranslationRepository(db *gorm.DB) *TranslationRepository {
	return &TranslationRepository{db: db}
}

// Lookup returns a cached translation for the exact source text and target
// language, if one exists.
func (r *TranslationRepository) Lookup(sourceHash, targetLang string) (string, bool) {
	var t models.Translation
	if err := r.db.Where("source_hash = ? AND target_lang = ?", sourceHash, targetLang).First(&t).Error; err != nil {
		return "", false
	}
	if strings.TrimSpace(t.Translated) == "" {
		return "", false
	}
	return t.Translated, true
}

// BulkLookup returns cached translations for a set of (hash, targetLang) pairs.
// The returned map is keyed by sourceHash; only hashes that have a cache hit are
// included. All entries share the same targetLang.
func (r *TranslationRepository) BulkLookup(hashes []string, targetLang string) map[string]string {
	if len(hashes) == 0 {
		return nil
	}
	// Build a map so we can preserve insertion order when returning results.
	existing := make(map[string]string, len(hashes))
	var rows []models.Translation
	r.db.Where("source_hash IN ? AND target_lang = ?", hashes, targetLang).
		Find(&rows)
	for _, t := range rows {
		if strings.TrimSpace(t.Translated) != "" {
			existing[t.SourceHash] = t.Translated
		}
	}
	return existing
}

// Store caches a translation. Re-translating an unchanged string is a no-op.
func (r *TranslationRepository) Store(t *models.Translation) error {
	if strings.TrimSpace(t.SourceHash) == "" || strings.TrimSpace(t.TargetLang) == "" {
		return fmt.Errorf("translation cache requires source hash and target language")
	}
	var count int64
	if err := r.db.Model(&models.Translation{}).
		Where("source_hash = ? AND target_lang = ?", t.SourceHash, t.TargetLang).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return r.db.Create(t).Error
}

// Count returns the number of cached translations.
func (r *TranslationRepository) Count() (int64, error) {
	var n int64
	err := r.db.Model(&models.Translation{}).Count(&n).Error
	return n, err
}

// CategoryRepository handles category operations.
type CategoryRepository struct{ db *gorm.DB }

func NewCategoryRepository(db *gorm.DB) *CategoryRepository { return &CategoryRepository{db: db} }

func (r *CategoryRepository) ListAll() ([]models.Category, error) {
	var cats []models.Category
	if err := r.db.Order("sort_order ASC, name ASC").Find(&cats).Error; err != nil {
		return nil, err
	}
	return cats, nil
}

func (r *CategoryRepository) GetBySlug(slug string) (*models.Category, error) {
	var c models.Category
	if err := r.db.Where("slug = ?", slug).First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *CategoryRepository) Create(c *models.Category) error { return r.db.Create(c).Error }

func (r *CategoryRepository) Update(c *models.Category) error { return r.db.Save(c).Error }

func (r *CategoryRepository) Delete(id uint) error { return r.db.Delete(&models.Category{}, id).Error }

// FilterRuleRepository handles filter rule operations.
type FilterRuleRepository struct{ db *gorm.DB }

func NewFilterRuleRepository(db *gorm.DB) *FilterRuleRepository { return &FilterRuleRepository{db: db} }

func (r *FilterRuleRepository) List() ([]models.SourceFilterRule, error) {
	var rules []models.SourceFilterRule
	if err := r.db.Order("created_at DESC").Find(&rules).Error; err != nil {
		return nil, err
	}
	return rules, nil
}

func (r *FilterRuleRepository) Create(rul *models.SourceFilterRule) error {
	return r.db.Create(rul).Error
}

func (r *FilterRuleRepository) Update(rul *models.SourceFilterRule) error {
	return r.db.Save(rul).Error
}

func (r *FilterRuleRepository) Delete(id uint) error {
	return r.db.Delete(&models.SourceFilterRule{}, id).Error
}

func (r *FilterRuleRepository) GetActiveBySourceID(sourceID uint) ([]models.SourceFilterRule, error) {
	var rules []models.SourceFilterRule
	if err := r.db.Where("(source_id IS NULL OR source_id = ?) AND is_active = ?", sourceID, true).Find(&rules).Error; err != nil {
		return nil, err
	}
	return rules, nil
}

// FollowRepository handles follow operations.
type FollowRepository struct{ db *gorm.DB }

func NewFollowRepository(db *gorm.DB) *FollowRepository { return &FollowRepository{db: db} }

func (r *FollowRepository) Add(f *models.Follow) error { return r.db.Create(f).Error }

func (r *FollowRepository) Remove(id uint) error { return r.db.Delete(&models.Follow{}, id).Error }

func (r *FollowRepository) IsFollowing(followType string, targetID uint) (bool, error) {
	var count int64
	if err := r.db.Model(&models.Follow{}).Where("follow_type = ? AND target_id = ?", followType, targetID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *FollowRepository) ListSources() ([]models.Source, error) {
	var sources []models.Source
	if err := r.db.Joins("JOIN follows ON follows.target_id = sources.id AND follows.follow_type = 'source'").
		Where("follows.deleted_at IS NULL").
		Order("sources.name ASC").Find(&sources).Error; err != nil {
		return nil, err
	}
	return sources, nil
}

// UserBehaviorRepository handles behavior tracking.
type UserBehaviorRepository struct{ db *gorm.DB }

func NewUserBehaviorRepository(db *gorm.DB) *UserBehaviorRepository {
	return &UserBehaviorRepository{db: db}
}

func (r *UserBehaviorRepository) Record(b *models.UserBehavior) error { return r.db.Create(b).Error }

func (r *UserBehaviorRepository) GetRecent(limit int) ([]models.UserBehavior, error) {
	var behaviors []models.UserBehavior
	if err := r.db.Order("created_at DESC").Limit(limit).Find(&behaviors).Error; err != nil {
		return nil, err
	}
	return behaviors, nil
}

// HotRankRepository handles hot ranking operations.
type HotRankRepository struct{ db *gorm.DB }

func NewHotRankRepository(db *gorm.DB) *HotRankRepository { return &HotRankRepository{db: db} }

func (r *HotRankRepository) ListTop(limit int) ([]models.HotRank, error) {
	var ranks []models.HotRank
	if err := r.db.Order("score DESC, rank ASC").Limit(limit).Find(&ranks).Error; err != nil {
		return nil, err
	}
	return ranks, nil
}

func (r *HotRankRepository) SaveOrUpdate(hr *models.HotRank) error {
	return r.db.Where("article_id = ?", hr.ArticleID).Assign(models.HotRank{Score: hr.Score, Rank: hr.Rank, SourceID: hr.SourceID, FetchedAt: hr.FetchedAt}).FirstOrCreate(hr).Error
}

// QueryTop returns one page of hot-ranked articles that match the article
// filter, so the hot-rank board honours exactly the same date/title/category
// filters as every other board.
//
// Rank is recomputed as the position inside the filtered result instead of
// being read from the stored snapshot: after filtering, a stored rank of 37
// sitting at the top of the list is just confusing. Score is kept as-is.
func (r *HotRankRepository) QueryTop(f models.NewsQuery) ([]models.RankWithArticle, int64, error) {
	f.Normalize()

	joinHot := func() *gorm.DB {
		return applyArticleFilter(r.db.Model(&models.Article{}).
			Joins("LEFT JOIN hot_ranks ON hot_ranks.article_id = articles.id AND hot_ranks.deleted_at IS NULL"), f)
	}

	var total int64
	if err := joinHot().Distinct("articles.id").Count(&total).Error; err != nil {
		return nil, 0, err
	}

	type row struct {
		ID    uint
		Score float64
	}
	var rows []row
	offset := (f.Page - 1) * f.PageSize
	if err := joinHot().
		Select("articles.id AS id, COALESCE(hot_ranks.score, 0) AS score").
		Order("score DESC").
		Order(articleTimeExpr + " DESC").
		Order("articles.id DESC").
		Offset(offset).Limit(f.PageSize).
		Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	if len(rows) == 0 {
		return []models.RankWithArticle{}, total, nil
	}

	ids := make([]uint, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}

	var articles []models.Article
	if err := r.db.Preload("Categories").Where("id IN ?", ids).Find(&articles).Error; err != nil {
		return nil, 0, err
	}
	byID := make(map[uint]models.Article, len(articles))
	for _, a := range articles {
		byID[a.ID] = a
	}

	out := make([]models.RankWithArticle, 0, len(rows))
	for i, row := range rows {
		a, ok := byID[row.ID]
		if !ok {
			continue
		}
		out = append(out, models.RankWithArticle{
			HotRank: models.HotRank{
				ArticleID: row.ID,
				Rank:      offset + i + 1,
				Score:     row.Score,
				SourceID:  a.SourceID,
				FetchedAt: a.FetchedAt,
			},
			Article: a,
		})
	}
	return out, total, nil
}

func (r *HotRankRepository) Recalculate() error {
	// Delete old ranks and recalculate
	return r.db.Exec("DELETE FROM hot_ranks").Error
}

// FetchLogRepository handles collection log operations.
type FetchLogRepository struct{ db *gorm.DB }

func NewFetchLogRepository(db *gorm.DB) *FetchLogRepository { return &FetchLogRepository{db: db} }

func (r *FetchLogRepository) Create(log *models.FetchLog) error { return r.db.Create(log).Error }

func (r *FetchLogRepository) List(page, pageSize int) ([]models.FetchLog, int64, error) {
	var logs []models.FetchLog
	var total int64
	r.db.Model(&models.FetchLog{}).Count(&total)
	offset := (page - 1) * pageSize
	if err := r.db.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	return logs, total, nil
}

// FilterLogRepository handles filter log operations.
type FilterLogRepository struct{ db *gorm.DB }

func NewFilterLogRepository(db *gorm.DB) *FilterLogRepository { return &FilterLogRepository{db: db} }

func (r *FilterLogRepository) Create(log *models.FilterLog) error { return r.db.Create(log).Error }

func (r *FilterLogRepository) List(page, pageSize int) ([]models.FilterLog, int64, error) {
	var logs []models.FilterLog
	var total int64
	r.db.Model(&models.FilterLog{}).Count(&total)
	offset := (page - 1) * pageSize
	if err := r.db.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	return logs, total, nil
}

// ArticleCategoryRepository handles article-category association operations.
type ArticleCategoryRepository struct{ db *gorm.DB }

func NewArticleCategoryRepository(db *gorm.DB) *ArticleCategoryRepository {
	return &ArticleCategoryRepository{db: db}
}

func (r *ArticleCategoryRepository) Save(articleID, categoryID uint) error {
	var count int64
	r.db.Model(&models.ArticleCategory{}).
		Where("article_id = ? AND category_id = ?", articleID, categoryID).
		Count(&count)
	if count == 0 {
		return r.db.Create(&models.ArticleCategory{ArticleID: articleID, CategoryID: categoryID}).Error
	}
	return nil
}

func (r *ArticleCategoryRepository) SaveBatch(items []models.ArticleCategory) error {
	if len(items) == 0 {
		return nil
	}
	return r.db.Create(&items).Error
}

// SystemSettingRepository handles system settings.
type SystemSettingRepository struct{ db *gorm.DB }

func NewSystemSettingRepository(db *gorm.DB) *SystemSettingRepository {
	return &SystemSettingRepository{db: db}
}

func (r *SystemSettingRepository) Get(key string) (*models.SystemSetting, error) {
	var s models.SystemSetting
	if err := r.db.Where("key = ?", key).First(&s).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *SystemSettingRepository) Set(key, value, description string) error {
	var s models.SystemSetting
	result := r.db.Where("key = ?", key).First(&s)
	if result.Error != nil {
		// Not found — create new
		return r.db.Create(&models.SystemSetting{Key: key, Value: value, Description: description}).Error
	}
	// Found — update existing
	return r.db.Model(&s).Updates(models.SystemSetting{Value: value, Description: description}).Error
}

func (r *SystemSettingRepository) GetAll() (map[string]string, error) {
	var settings []models.SystemSetting
	if err := r.db.Find(&settings).Error; err != nil {
		return nil, err
	}
	m := make(map[string]string)
	for _, s := range settings {
		m[s.Key] = s.Value
	}
	return m, nil
}
