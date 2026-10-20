package classifier

// extraKeywords supplements what a category's slug, name and description provide.
//
// Deriving keywords from the human-readable description alone is unreliable: the
// Chinese descriptions have no spaces, so a naive split produced one giant
// unusable token, and the English descriptions are prose ("Expert views,
// research reports") rather than search terms. These lists are the actual
// classification vocabulary, kept out of the UI text on purpose.
//
// Order does not matter; every term is matched case-insensitively. Chinese terms
// are matched as substrings (correct for a language without word separators),
// ASCII terms as whole words (so "AI" no longer matches "said" or "campaign").
var extraKeywords = map[string][]string{
	// ---- Finance ----
	"finance-macro": {
		"GDP", "CPI", "PPI", "PMI", "通胀", "通货膨胀", "物价", "失业率", "采购经理",
		"经济增速", "国内生产总值", "社会消费品零售", "固定资产投资", "工业增加值",
		"经济数据", "经济报告", "经济预测", "经济表现", "经济景气",
		"inflation", "unemployment", "trade balance", "consumer price", "consumer prices",
		"producer price", "producer prices", "industrial production", "industrial output",
		"purchasing managers", "gross domestic product", "economic growth",
		"economic data", "economic report", "economic forecast", "economic performance",
	},
	"finance-securities": {
		"股指", "上证", "深证", "恒生", "纳斯达克", "道琼斯", "标普", "股价", "涨停",
		"跌停", "上市公司", "财报", "业绩预告", "并购重组", "分红", "券商", "A股", "港股",
		"美股", "IPO", "股市", "大盘", "指数", "股票", "股权投资", "权益",
		"stock index", "earnings", "shares", "broker", "listed company",
		"stock market", "equity", "wall street",
	},
	"finance-fixed-income-forex": {
		"国债", "收益率", "债券", "汇率", "人民币汇率", "美元指数", "外汇储备", "企业债",
		"信用债", "债券违约", "外汇", "美元", "欧元", "日元", "英镑", "货币",
		"treasury", "bond yield", "forex", "exchange rate", "reserves",
		"dollar", "euro", "yen", "pound", "currency",
	},
	"finance-commodities": {
		"原油", "天然气", "煤炭", "黄金", "白银", "铜价", "铝价", "镍", "大豆", "玉米",
		"小麦", "棉花", "期货", "现货", "库存", "农产品", "金属", "能源", "石油价格",
		"commodity", "crude oil", "gold price",
		"natural gas", "copper", "wheat", "soybean", "oil price",
	},
	"finance-industry": {
		"新能源", "半导体", "房地产", "医药", "航运", "钢铁", "产业链", "产能", "龙头企业",
		"行业政策", "房价", "楼市", "supply chain", "industry policy", "capacity",
		"real estate", "residential", "property price", "commercial residential",
	},
	"finance-banking-insurance": {
		"存款利率", "不良贷款", "信贷", "银行", "保险", "保费", "偿付能力", "基金", "资管",
		"公募", "私募", "净值", "存款", "贷款", "信贷", "信用", "利息", "利率",
		"bank", "insurer", "insurance", "asset management", "mutual fund",
		"deposit", "loan", "credit", "interest rate",
	},
	"finance-intl-trade": {
		"进出口", "关税", "贸易协定", "贸易摩擦", "跨境投资", "海外建厂", "顺差", "逆差",
		"外贸", "tariff", "import", "export", "trade deal", "trade war", "customs",
	},
	"finance-commentary": {
		"研报", "投行", "专家观点", "智库报告", "银行危机", "债务危机", "通胀危机",
		"市场分析", "市场评论", "投资观点", "投资策略",
		"research note", "investment bank", "market outlook", "analyst",
		"market analysis", "market commentary", "investment strategy",
	},
	"finance-regulation": {
		"监管", "反垄断", "金融科技", "财税改革", "金融风险", "罚款", "处罚", "新规",
		"证监会", "银保监会", "央行加息", "央行降息", "货币政策",
		"regulator", "antitrust", "financial regulation", "fintech", "crackdown",
	},

	// ---- Military ----
	"military-defense-policy": {
		"国防预算", "军费", "国防白皮书", "国家安全战略", "军队改革", "兵役", "国防部",
		"战略", "国防政策", "军事战略", "国家安全", "defense budget", "defence budget",
		"national security strategy", "conscription", "military reform", "pentagon",
		"strategic", "defense policy", "national defense",
	},
	"military-equipment": {
		"战机", "舰艇", "坦克", "导弹", "防空系统", "无人机", "高超音速", "潜艇", "军售",
		"武器出口", "军工", "雷达", "卫星", "导弹防御", "战斗机", "驱逐舰", "巡洋舰",
		"fighter jet", "warship", "missile", "submarine", "drone",
		"hypersonic", "arms sale", "weapons", "radar", "satellite", "missile defense",
		"fighter", "destroyer", "cruiser",
	},
	"military-exercises": {
		"军演", "演习", "联合演习", "兵力调动", "驻军", "军事基地", "部署",
		"军事演习", "联合军演", "military exercise", "war games", "troop deployment",
		"garrison", "air base", "naval base", "joint exercise",
	},
	"military-conflicts": {
		"交火", "战线", "停火", "战俘", "冲突", "边境", "对峙", "空袭", "战事",
		"战争", "战斗", "轰炸", "炮击", "进攻", "防御", "战场",
		"ceasefire", "front line", "airstrike", "offensive", "shelling", "clashes",
		"border tension", "prisoners of war", "war", "combat", "bombing", "battlefield",
	},
	"military-personnel": {
		"国防部长", "参谋长", "将领", "任免", "军方", "将军", "司令", "总参谋长",
		"defense minister", "chief of staff", "general", "military leadership",
		"commander", "general officer", "military chief",
	},
	"military-arm-control": {
		"核试验", "核裁军", "核不扩散", "军控", "核武器", "反导", "生化武器",
		"核弹头", "核威慑", "核力量", "nuclear test", "nuclear weapon", "arms control",
		"nonproliferation", "disarmament", "missile defense", "nuclear warhead",
		"nuclear deterrence", "nuclear arsenal",
	},
	"military-security": {
		"反恐", "反海盗", "海上安全", "航道安全", "空域", "军机", "军火走私",
		"网络安全", "网络战", "cybersecurity", "cyber warfare",
		"counterterrorism", "counter-terrorism", "anti-piracy", "maritime security",
		"smuggling", "cyber attack", "cyber defense",
	},
	"military-history": {
		"战史", "军事历史", "防务智库", "军力评估", "军力对比", "二战", "战争史",
		"military history", "think tank", "force posture", "war college",
		"world war", "military history", "defense analysis",
	},
	"military-disaster": {
		"救灾", "撤侨", "人道主义救援", "救援行动", "disaster relief", "evacuation",
		"humanitarian mission",
	},

	// ---- Technology ----
	"tech-science": {
		"天文", "航天", "卫星", "火箭", "空间站", "物理", "化学", "生物", "论文", "实验",
		"地质", "古生物", "望远镜", "研究", "科学家", "research", "scientist",
		"spacecraft", "satellite", "rocket launch", "telescope",
		"physics", "biology", "peer-reviewed", "astronomy",
	},
	"tech-it-ai": {
		"大模型", "人工智能", "芯片", "半导体", "光刻机", "操作系统", "开源", "云计算",
		"数据中心", "网络安全", "漏洞", "黑客", "算法", "算力", "AI", "GPU", "LLM",
		"深度学习", "自然语言处理", "机器学习", "神经网络",
		"machine learning", "neural network", "cybersecurity", "vulnerability", "open source",
		"cloud computing", "datacenter", "software", "deep learning", "NLP",
	},
	"tech-hardware": {
		"手机", "电脑", "服务器", "机器人", "无人机", "VR", "AR", "可穿戴", "传感器",
		"电池", "新材料", "芯片", "处理器", "smartphone", "laptop", "wearable",
		"robot", "gadget", "gpu", "chip", "processor",
	},
	"tech-biomed": {
		"新药", "临床试验", "疫苗", "基因", "细胞治疗", "医疗器械", "公共卫生", "抗体",
		"疾病", "治疗", "疗法", "药物", "医疗", "health", "disease", "treatment",
		"clinical trial", "vaccine", "gene therapy", "drug approval", "medical device",
	},
	"tech-green-energy": {
		"光伏", "风电", "储能", "氢能", "核聚变", "电动汽车", "固态电池", "碳捕集",
		"碳中和", "清洁能源", "可再生能源", "solar", "wind power", "battery storage",
		"hydrogen", "fusion", "electric vehicle", "carbon capture", "renewable",
		"clean energy", "renewable energy",
	},
	"tech-manufacturing": {
		"智能制造", "工业机器人", "3D打印", "工业自动化", "先进制造", "工艺流程",
		"工厂", "生产线", "制造", "工业", "manufacturing", "industrial robot",
		"3d printing", "automation", "factory", "production line", "manufacturing",
	},
	"tech-policy": {
		"科技法案", "出口管制", "知识产权", "科研经费", "技术封锁", "技术标准",
		"数据隐私", "数字税", "数据保护", "数字监管",
		"export control", "patent", "technology policy", "research funding", "sanction",
		"data privacy", "digital tax", "data protection",
	},
	"tech-companies": {
		"新品发布", "研发投入", "技术并购", "产品线", "科技公司", "product launch",
		"r&d spending", "acquisition", "startup", "chipmaker",
		"苹果", "谷歌", "微软", "亚马逊", "Meta", "特斯拉", "Apple", "Google",
		"Microsoft", "Amazon", "Meta", "Tesla",
	},
	"tech-frontier": {
		"量子计算", "脑机接口", "合成生物学", "量子通信", "quantum computing",
		"brain-computer", "synthetic biology", "quantum",
		"AI safety", "AI alignment", "artificial general intelligence",
	},
	"tech-risks": {
		"技术事故", "实验室安全", "AI风险", "技术伦理", "数据泄露", "隐私",
		"网络攻击", "黑客攻击", "数据泄露", "网络安全事件",
		"data breach", "ai risk", "tech accident", "privacy", "lab safety",
		"cyberattack", "data breach", "security incident",
	},

	// ---- Politics ----
	"politics-domestic": {
		"选举", "大选", "议会", "内阁", "弹劾", "罢免", "法案", "公投", "抗议", "示威",
		"政党", "政府", "总统", "总理", "首相", "议员", "立法", "宪法", "司法",
		"election", "parliament", "cabinet", "impeachment", "referendum",
		"protest", "legislation", "senate", "government", "president", "prime minister",
		"leader", "legislature", "constitution", "judiciary",
	},
	"politics-diplomacy": {
		"会晤", "双边会谈", "国事访问", "联合声明", "联合国大会", "安理会", "峰会",
		"制裁", "断交", "建交", "大使", "外交", "联合国", "北约", "欧盟",
		"summit", "bilateral", "diplomat", "state visit", "sanctions", "united nations",
		"foreign minister", "embassy", "UN", "NATO", "EU", "diplomatic",
	},
	"politics-geopolitics": {
		"地缘", "领土争端", "地区局势", "边境争端", "和平协议", "地区安全", "地缘政治",
		"一带一路", "亚太", "中东", "欧洲", "东南亚", "南亚", "朝鲜", "韩国", "台湾",
		"南海", "台海",
		"geopolitic", "territorial", "territorial dispute", "regional security", "peace deal",
		"Belt and Road", "Asia-Pacific", "Middle East", "Southeast Asia", "Taiwan",
		"South China Sea",
	},
	"politics-governance": {
		"国际公约", "全球治理", "人权", "气候治理", "国际规则", "联合国", "WTO", "WHO",
		"human rights", "global governance", "international convention", "climate summit",
		"United Nations", "WTO", "WHO", "World Health",
	},
	"politics-commentary": {
		"政局", "政治分析", "政策解读", "political analysis", "think tank", "policy brief",
		"pundit",
	},

	// ---- Economy ----
	"economy-indicators": {
		"GDP", "CPI", "PPI", "失业率", "工业增加值", "社会消费品零售", "固定资产投资",
		"产能利用率", "人均GDP", "经济增速", "居民消费价格", "工业生产者价格",
		"经济数据", "经济指标", "经济报告",
		"consumer price", "consumer prices", "producer price", "producer prices",
		"purchasing managers", "gross domestic product", "industrial output",
		"industrial production", "retail sales", "fixed asset investment", "economic growth",
		"economic data", "economic indicator", "economic report",
	},
	"economy-monetary-fiscal": {
		"央行", "利率", "存款准备金", "货币供应", "M2", "财政预算", "财政赤字", "国债发行",
		"税收", "货币政策", "财政政策", "加息", "降息", "量化宽松", "QE",
		"central bank", "interest rate", "fiscal deficit",
		"monetary policy", "tax", "budget", "rate cut", "rate hike",
		"fiscal policy", "quantitative easing",
	},
	"economy-regional-global": {
		"经济展望", "IMF", "世界银行", "经合组织", "经济衰退", "经济复苏", "区域经济",
		"全球经济", "世界经济", "新兴市场", "发达经济体",
		"economic outlook", "world bank", "recession", "recovery", "oecd", "imf",
		"global economy", "emerging market", "developed economy",
	},
	"economy-industry": {
		"供需", "产能过剩", "产业政策", "产业转移", "产业链", "行业", "产业", "工业",
		"industrial policy", "overcapacity", "supply and demand",
		"industry", "manufacturing sector",
	},
	"economy-demographics": {
		"出生率", "老龄化", "人口迁移", "居民收入", "储蓄", "房价", "杠杆", "人口",
		"人口数据", "人口结构", "劳动力", "就业", "工资",
		"birth rate", "aging population", "household income", "housing price",
		"demographics", "labor force", "employment", "wage",
	},
	"economy-policy": {
		"经济改革", "反垄断", "市场准入", "营商环境", "经济工作会议", "产业规划",
		"经济政策", "经济体制", "市场化", "自由化",
		"economic reform", "market access", "business environment", "deregulation",
		"economic policy", "marketization",
	},
	"economy-research": {
		"经济预测", "债务风险", "通胀风险", "通缩", "房地产风险", "风险预警",
		"经济研究", "经济分析", "经济报告", "经济展望",
		"economic forecast", "debt risk", "deflation", "risk warning", "downgrade",
		"economic research", "economic analysis",
	},
}
