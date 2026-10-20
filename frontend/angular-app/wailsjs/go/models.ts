export namespace bindings {
	
	export class TTSArticleStatusResult {
	    hasAudio: boolean;
	    synthesizing: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TTSArticleStatusResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hasAudio = source["hasAudio"];
	        this.synthesizing = source["synthesizing"];
	    }
	}

}

export namespace classifier {
	
	export class AuditCategoryStat {
	    categoryId: number;
	    category: string;
	    total: number;
	    weak: number;
	
	    static createFrom(source: any = {}) {
	        return new AuditCategoryStat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.categoryId = source["categoryId"];
	        this.category = source["category"];
	        this.total = source["total"];
	        this.weak = source["weak"];
	    }
	}
	export class AuditCounts {
	    total: number;
	    unclassified: number;
	
	    static createFrom(source: any = {}) {
	        return new AuditCounts(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.unclassified = source["unclassified"];
	    }
	}
	export class AuditItem {
	    articleId: number;
	    title: string;
	    categoryId: number;
	    category: string;
	    keyword: string;
	    hitCount: number;
	
	    static createFrom(source: any = {}) {
	        return new AuditItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.articleId = source["articleId"];
	        this.title = source["title"];
	        this.categoryId = source["categoryId"];
	        this.category = source["category"];
	        this.keyword = source["keyword"];
	        this.hitCount = source["hitCount"];
	    }
	}
	export class AuditResult {
	    categories: AuditCategoryStat[];
	    suspects: AuditItem[];
	    counts: AuditCounts;
	
	    static createFrom(source: any = {}) {
	        return new AuditResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.categories = this.convertValues(source["categories"], AuditCategoryStat);
	        this.suspects = this.convertValues(source["suspects"], AuditItem);
	        this.counts = this.convertValues(source["counts"], AuditCounts);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace llmqueue {
	
	export class Event {
	    id: number;
	    // Go type: time
	    time: any;
	    jobId: number;
	    kind: string;
	    jobName: string;
	    type: string;
	    articleId?: number;
	    title?: string;
	    detail?: string;
	    count?: number;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new Event(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.time = this.convertValues(source["time"], null);
	        this.jobId = source["jobId"];
	        this.kind = source["kind"];
	        this.jobName = source["jobName"];
	        this.type = source["type"];
	        this.articleId = source["articleId"];
	        this.title = source["title"];
	        this.detail = source["detail"];
	        this.count = source["count"];
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class JobInfo {
	    id: number;
	    kind: string;
	    name: string;
	    // Go type: time
	    startedAt: any;
	    item?: string;
	    done?: number;
	    total?: number;
	
	    static createFrom(source: any = {}) {
	        return new JobInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.name = source["name"];
	        this.startedAt = this.convertValues(source["startedAt"], null);
	        this.item = source["item"];
	        this.done = source["done"];
	        this.total = source["total"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Status {
	    queued: number;
	    current?: JobInfo;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.queued = source["queued"];
	        this.current = this.convertValues(source["current"], JobInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace models {
	
	export class Category {
	    ID: number;
	    // Go type: time
	    CreatedAt: any;
	    // Go type: time
	    UpdatedAt: any;
	    // Go type: gorm
	    DeletedAt: any;
	    name: string;
	    nameEn: string;
	    slug: string;
	    parentId?: number;
	    sortOrder: number;
	    description: string;
	    descriptionEn: string;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new Category(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.CreatedAt = this.convertValues(source["CreatedAt"], null);
	        this.UpdatedAt = this.convertValues(source["UpdatedAt"], null);
	        this.DeletedAt = this.convertValues(source["DeletedAt"], null);
	        this.name = source["name"];
	        this.nameEn = source["nameEn"];
	        this.slug = source["slug"];
	        this.parentId = source["parentId"];
	        this.sortOrder = source["sortOrder"];
	        this.description = source["description"];
	        this.descriptionEn = source["descriptionEn"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Article {
	    ID: number;
	    // Go type: time
	    CreatedAt: any;
	    // Go type: time
	    UpdatedAt: any;
	    // Go type: gorm
	    DeletedAt: any;
	    sourceId: number;
	    title: string;
	    summary: string;
	    lead: string;
	    originUrl: string;
	    publisher: string;
	    // Go type: time
	    publishTime: any;
	    // Go type: time
	    fetchedAt: any;
	    fingerprint: string;
	    // Go type: time
	    expireAt: any;
	    locations: string;
	    persons: string;
	    events: string;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	    lang: string;
	    titleZh: string;
	    titleEn: string;
	    summaryZh: string;
	    summaryEn: string;
	    leadZh: string;
	    leadEn: string;
	    transStatus: string;
	    transAttempts: number;
	    transError: string;
	    // Go type: time
	    translatedAt?: any;
	    classifiedBy: string;
	    categories?: Category[];
	
	    static createFrom(source: any = {}) {
	        return new Article(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.CreatedAt = this.convertValues(source["CreatedAt"], null);
	        this.UpdatedAt = this.convertValues(source["UpdatedAt"], null);
	        this.DeletedAt = this.convertValues(source["DeletedAt"], null);
	        this.sourceId = source["sourceId"];
	        this.title = source["title"];
	        this.summary = source["summary"];
	        this.lead = source["lead"];
	        this.originUrl = source["originUrl"];
	        this.publisher = source["publisher"];
	        this.publishTime = this.convertValues(source["publishTime"], null);
	        this.fetchedAt = this.convertValues(source["fetchedAt"], null);
	        this.fingerprint = source["fingerprint"];
	        this.expireAt = this.convertValues(source["expireAt"], null);
	        this.locations = source["locations"];
	        this.persons = source["persons"];
	        this.events = source["events"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	        this.lang = source["lang"];
	        this.titleZh = source["titleZh"];
	        this.titleEn = source["titleEn"];
	        this.summaryZh = source["summaryZh"];
	        this.summaryEn = source["summaryEn"];
	        this.leadZh = source["leadZh"];
	        this.leadEn = source["leadEn"];
	        this.transStatus = source["transStatus"];
	        this.transAttempts = source["transAttempts"];
	        this.transError = source["transError"];
	        this.translatedAt = this.convertValues(source["translatedAt"], null);
	        this.classifiedBy = source["classifiedBy"];
	        this.categories = this.convertValues(source["categories"], Category);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ArticleListResult {
	    articles: Article[];
	    total: number;
	
	    static createFrom(source: any = {}) {
	        return new ArticleListResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.articles = this.convertValues(source["articles"], Article);
	        this.total = source["total"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class HotRank {
	    ID: number;
	    // Go type: time
	    CreatedAt: any;
	    // Go type: time
	    UpdatedAt: any;
	    // Go type: gorm
	    DeletedAt: any;
	    articleId: number;
	    rank: number;
	    score: number;
	    sourceId: number;
	    // Go type: time
	    fetchedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new HotRank(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.CreatedAt = this.convertValues(source["CreatedAt"], null);
	        this.UpdatedAt = this.convertValues(source["UpdatedAt"], null);
	        this.DeletedAt = this.convertValues(source["DeletedAt"], null);
	        this.articleId = source["articleId"];
	        this.rank = source["rank"];
	        this.score = source["score"];
	        this.sourceId = source["sourceId"];
	        this.fetchedAt = this.convertValues(source["fetchedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class NewsQuery {
	    page: number;
	    pageSize: number;
	    categorySlug: string;
	    categoryPrefix: string;
	    sourceId: number;
	    followOnly: boolean;
	    titleKeyword: string;
	    dateFrom: string;
	    dateTo: string;
	    datePreset: string;
	    sortBy: string;
	
	    static createFrom(source: any = {}) {
	        return new NewsQuery(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.page = source["page"];
	        this.pageSize = source["pageSize"];
	        this.categorySlug = source["categorySlug"];
	        this.categoryPrefix = source["categoryPrefix"];
	        this.sourceId = source["sourceId"];
	        this.followOnly = source["followOnly"];
	        this.titleKeyword = source["titleKeyword"];
	        this.dateFrom = source["dateFrom"];
	        this.dateTo = source["dateTo"];
	        this.datePreset = source["datePreset"];
	        this.sortBy = source["sortBy"];
	    }
	}
	export class RankWithArticle {
	    HotRank: HotRank;
	    Article: Article;
	
	    static createFrom(source: any = {}) {
	        return new RankWithArticle(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.HotRank = this.convertValues(source["HotRank"], HotRank);
	        this.Article = this.convertValues(source["Article"], Article);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RankListResult {
	    ranks: RankWithArticle[];
	    total: number;
	
	    static createFrom(source: any = {}) {
	        return new RankListResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ranks = this.convertValues(source["ranks"], RankWithArticle);
	        this.total = source["total"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class RawNewsItem {
	    Title: string;
	    Summary: string;
	    Lead: string;
	    // Go type: time
	    PublishTime: any;
	    OriginURL: string;
	    Locations: string[];
	    Persons: string[];
	    Events: string[];
	    Publisher: string;
	
	    static createFrom(source: any = {}) {
	        return new RawNewsItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Title = source["Title"];
	        this.Summary = source["Summary"];
	        this.Lead = source["Lead"];
	        this.PublishTime = this.convertValues(source["PublishTime"], null);
	        this.OriginURL = source["OriginURL"];
	        this.Locations = source["Locations"];
	        this.Persons = source["Persons"];
	        this.Events = source["Events"];
	        this.Publisher = source["Publisher"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Source {
	    ID: number;
	    // Go type: time
	    CreatedAt: any;
	    // Go type: time
	    UpdatedAt: any;
	    // Go type: gorm
	    DeletedAt: any;
	    name: string;
	    type: string;
	    url: string;
	    isActive: boolean;
	    cronExpression: string;
	    requestInterval: number;
	    bindFilterRuleId?: number;
	    publisher: string;
	    status: string;
	    lastError: string;
	    // Go type: time
	    lastFetchedAt?: any;
	    sampleData: string;
	    fetchCount: number;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new Source(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.CreatedAt = this.convertValues(source["CreatedAt"], null);
	        this.UpdatedAt = this.convertValues(source["UpdatedAt"], null);
	        this.DeletedAt = this.convertValues(source["DeletedAt"], null);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.url = source["url"];
	        this.isActive = source["isActive"];
	        this.cronExpression = source["cronExpression"];
	        this.requestInterval = source["requestInterval"];
	        this.bindFilterRuleId = source["bindFilterRuleId"];
	        this.publisher = source["publisher"];
	        this.status = source["status"];
	        this.lastError = source["lastError"];
	        this.lastFetchedAt = this.convertValues(source["lastFetchedAt"], null);
	        this.sampleData = source["sampleData"];
	        this.fetchCount = source["fetchCount"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TestResult {
	    available: boolean;
	    message: string;
	    samples?: RawNewsItem[];
	
	    static createFrom(source: any = {}) {
	        return new TestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.message = source["message"];
	        this.samples = this.convertValues(source["samples"], RawNewsItem);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TranslationStats {
	    total: number;
	    pending: number;
	    processable: number;
	    done: number;
	    failed: number;
	    skipped: number;
	    cacheHit: number;
	
	    static createFrom(source: any = {}) {
	        return new TranslationStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.pending = source["pending"];
	        this.processable = source["processable"];
	        this.done = source["done"];
	        this.failed = source["failed"];
	        this.skipped = source["skipped"];
	        this.cacheHit = source["cacheHit"];
	    }
	}

}

export namespace translator {
	
	export class LocalModel {
	    name: string;
	    baseUrl: string;
	    models: string[];
	
	    static createFrom(source: any = {}) {
	        return new LocalModel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.baseUrl = source["baseUrl"];
	        this.models = source["models"];
	    }
	}
	export class Status {
	    configured: boolean;
	    engine: string;
	    provider: string;
	    model: string;
	    baseUrl: string;
	    local: boolean;
	    scope: string;
	    dailyLimit: number;
	    translatedToday: number;
	    reachable: boolean;
	    isRunning: boolean;
	    // Go type: time
	    lastRun: any;
	    lastError: string;
	    lastCount: number;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.configured = source["configured"];
	        this.engine = source["engine"];
	        this.provider = source["provider"];
	        this.model = source["model"];
	        this.baseUrl = source["baseUrl"];
	        this.local = source["local"];
	        this.scope = source["scope"];
	        this.dailyLimit = source["dailyLimit"];
	        this.translatedToday = source["translatedToday"];
	        this.reachable = source["reachable"];
	        this.isRunning = source["isRunning"];
	        this.lastRun = this.convertValues(source["lastRun"], null);
	        this.lastError = source["lastError"];
	        this.lastCount = source["lastCount"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace tts {
	
	export class PlaylistItem {
	    articleId: number;
	    title: string;
	    url: string;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new PlaylistItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.articleId = source["articleId"];
	        this.title = source["title"];
	        this.url = source["url"];
	        this.source = source["source"];
	    }
	}
	export class Status {
	    enabled: boolean;
	    state: string;
	    total: number;
	    done: number;
	    failed: number;
	    roundId: number;
	    // Go type: time
	    finishedAt: any;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.state = source["state"];
	        this.total = source["total"];
	        this.done = source["done"];
	        this.failed = source["failed"];
	        this.roundId = source["roundId"];
	        this.finishedAt = this.convertValues(source["finishedAt"], null);
	        this.message = source["message"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

