import { Injectable, signal } from '@angular/core';
import { NewsDates, NewsGet, NewsListCategories, NewsSearch } from '../../../wailsjs/go/bindings/Bindings';
import { models } from '../../../wailsjs/go/models';

export interface Category {
  id: number;
  name: string;
  nameEn: string;
  slug: string;
  parentId?: number;
  sortOrder: number;
  description: string;
  descriptionEn: string;
  createdAt: string;
  updatedAt: string;
}

export interface Article {
  id: number;
  sourceId: number;
  /** Original text exactly as fetched; never overwritten by a translation. */
  title: string;
  summary: string;
  lead: string;
  /** Detected language of the original: 'zh' | 'en' | ''. */
  lang: string;
  titleZh: string;
  titleEn: string;
  summaryZh: string;
  summaryEn: string;
  leadZh: string;
  leadEn: string;
  /** pending | done | failed | skipped */
  transStatus: string;
  transError: string;
  originUrl: string;
  publisher: string;
  publishTime: string;
  fetchedAt: string;
  fingerprint: string;
  expireAt: string;
  locations: string;
  persons: string;
  events: string;
  categories?: Category[];
  /** '' | 'keyword' | 'llm' — how the article was classified. */
  classifiedBy: string;
  createdAt: string;
  updatedAt: string;
}

export interface NewsListResult {
  articles: Article[];
  total: number;
}

/** Date presets understood by the backend (models.NewsQuery.DatePreset). */
export type DatePreset = 'today' | '7d' | '30d' | 'all' | 'custom';
export type SortBy = 'time' | 'hot';

/**
 * The single filter/sort/pagination request shared by every board. It mirrors
 * models.NewsQuery on the Go side field for field, so the filter bar behaves
 * identically wherever it is used.
 */
export interface NewsQuery {
  page: number;
  pageSize: number;
  categorySlug?: string;
  categoryPrefix?: string;
  sourceId?: number;
  followOnly?: boolean;
  titleKeyword?: string;
  dateFrom?: string;
  dateTo?: string;
  datePreset?: DatePreset;
  sortBy?: SortBy;
}

/** Default filter for every board: today, newest first. */
export function defaultQuery(extra: Partial<NewsQuery> = {}): NewsQuery {
  return {
    page: 1,
    pageSize: 20,
    datePreset: 'today',
    sortBy: 'time',
    titleKeyword: '',
    ...extra,
  };
}

/**
 * Single shared mapper from the generated Wails model to the UI Article shape.
 *
 * Exported on purpose: recommend.service.ts and hotrank.service.ts used to keep
 * private copies, and every copy silently lost the `categories` relation — which
 * is why article classification never showed up in the UI. Keep one mapper.
 */
export function toArticle(a: models.Article): Article {
  return {
    id: a.ID,
    sourceId: a.sourceId,
    title: a.title,
    summary: a.summary,
    lead: a.lead,
    lang: a.lang || '',
    titleZh: a.titleZh || '',
    titleEn: a.titleEn || '',
    summaryZh: a.summaryZh || '',
    summaryEn: a.summaryEn || '',
    leadZh: a.leadZh || '',
    leadEn: a.leadEn || '',
    transStatus: a.transStatus || '',
    transError: a.transError || '',
    originUrl: a.originUrl,
    publisher: a.publisher || '',
    publishTime: a.publishTime ? new Date(a.publishTime).toISOString() : '',
    fetchedAt: a.fetchedAt ? new Date(a.fetchedAt).toISOString() : '',
    fingerprint: a.fingerprint,
    expireAt: a.expireAt ? new Date(a.expireAt).toISOString() : '',
    locations: a.locations,
    persons: a.persons,
    events: a.events,
    categories: (a.categories || []).map(toCategory),
    classifiedBy: a.classifiedBy || '',
    createdAt: a.createdAt ? new Date(a.createdAt).toISOString() : '',
    updatedAt: a.updatedAt ? new Date(a.updatedAt).toISOString() : '',
  };
}

export function toCategory(c: models.Category): Category {
  return {
    id: c.ID,
    name: c.name,
    nameEn: c.nameEn || '',
    slug: c.slug,
    parentId: c.parentId || undefined,
    sortOrder: c.sortOrder,
    description: c.description,
    descriptionEn: c.descriptionEn || '',
    createdAt: c.createdAt ? new Date(c.createdAt).toISOString() : '',
    updatedAt: c.updatedAt ? new Date(c.updatedAt).toISOString() : '',
  };
}

/** Category label in the requested language, falling back to Chinese. */
export function categoryLabel(c: Category, lang: 'zh' | 'en'): string {
  return lang === 'en' ? c.nameEn || c.name : c.name;
}

/** Category description in the requested language. */
export function categoryDescription(c: Category, lang: 'zh' | 'en'): string {
  return lang === 'en' ? c.descriptionEn || c.description : c.description;
}

/**
 * Title/summary/lead in the requested language, falling back to the original
 * text when the translation is not there yet. Returns which side was used so the
 * UI can label it honestly instead of passing a translation off as the original.
 */
export interface Variant {
  title: string;
  summary: string;
  lead: string;
  isTranslation: boolean;
  missing: boolean;
}

export function articleVariant(a: Article, lang: 'zh' | 'en'): Variant {
  const title = lang === 'zh' ? a.titleZh : a.titleEn;
  const summary = lang === 'zh' ? a.summaryZh : a.summaryEn;
  const lead = lang === 'zh' ? a.leadZh : a.leadEn;

  if (title) {
    return { title, summary, lead, isTranslation: a.lang !== lang, missing: false };
  }

  // The original language could not be determined (an all-numeric headline, for
  // example). There is nothing to translate from, so present the text as-is
  // rather than promising a translation that will never arrive.
  if (!a.lang) {
    return { title: a.title, summary: a.summary, lead: a.lead, isTranslation: false, missing: false };
  }

  // No variant in the requested language: show the original so the card is never
  // empty, and let the caller show the "not translated yet" hint.
  return {
    title: a.title,
    summary: a.summary,
    lead: a.lead,
    isTranslation: false,
    missing: a.title !== '',
  };
}

@Injectable({ providedIn: 'root' })
export class NewsService {
  private articles = signal<Article[]>([]);
  private categories = signal<Category[]>([]);
  articles$ = this.articles.asReadonly();
  categories$ = this.categories.asReadonly();

  /** Runs a filtered query. This is what every board calls. */
  async search(query: NewsQuery): Promise<NewsListResult> {
    // Spread first, then apply the defaults, so an explicit page/pageSize in the
    // caller's query still wins.
    const q: NewsQuery = {
      ...query,
      page: query.page || 1,
      pageSize: query.pageSize || 20,
      datePreset: query.datePreset || 'today',
      sortBy: query.sortBy || 'time',
    };
    try {
      const result = await NewsSearch(q as any);
      const articles = (result?.articles || []).map(toArticle);
      this.articles.set(articles);
      console.debug(`[NewsService] search ${JSON.stringify(q)} -> ${articles.length}/${result?.total ?? articles.length}`);
      return { articles, total: result?.total ?? articles.length };
    } catch (err) {
      // Never let a failed query look like "no news": a silently swallowed
      // error here previously masked a broken backend relation.
      console.error(`[NewsService] search FAILED ${JSON.stringify(q)}:`, err);
      this.articles.set([]);
      return { articles: [], total: 0 };
    }
  }

  /**
   * Dates that actually contain articles, newest first, for the date picker.
   */
  async availableDates(limit: number = 60): Promise<string[]> {
    try {
      return (await NewsDates(limit)) || [];
    } catch (err) {
      console.error('[NewsService] availableDates FAILED:', err);
      return [];
    }
  }

  async get(id: number): Promise<Article> {
    const result = await NewsGet(id);
    return toArticle(result);
  }

  async listCategories(): Promise<Category[]> {
    const result = await NewsListCategories();
    const categories = (result || []).map(toCategory);
    this.categories.set(categories);
    return categories;
  }

  openExternal(url: string): void {
    window.open(url, '_blank');
  }
}
