import { Component, Input, OnInit, SimpleChanges, signal } from '@angular/core';
import { Article, NewsQuery, NewsService, SortBy, defaultQuery } from '../../services/news.service';
import { DataSourceService } from '../../services/datasource.service';
import { I18nService } from '../../services/i18n.service';
import { TranslationService } from '../../services/translation.service';
import { NewsCardComponent } from '../news-card/news-card.component';
import { NewsFilterComponent } from '../news-filter/news-filter.component';

/** Anything that can answer a filtered article query. */
export type ArticleFetcher = (query: NewsQuery) => Promise<{ articles: Article[]; total: number }>;

/**
 * The shared article board: filter bar, list, pagination.
 *
 * Every board used to reimplement this loop with its own copy of the state, and
 * the copies drifted (the topic boards always loaded page 1 regardless of the
 * page counter). Boards now only supply a fetcher and their empty-state text.
 *
 * The fixed criteria are SCALAR inputs on purpose. An object input such as
 * `[base]="{ sortBy: 'hot' }"` produces a brand new object on every change
 * detection pass, so any ngOnChanges-driven reload would loop forever.
 */
@Component({
  selector: 'article-board',
  standalone: false,
  template: `
    <news-filter
      [defaultSort]="defaultSort"
      [total]="total()"
      (queryChange)="onQuery($event)"
    ></news-filter>

    @if (loading()) {
      <div class="loading">{{ i18n.t('common.loading') }}</div>
    } @else {
      @for (article of articles(); track article.id; let i = $index) {
        <div class="article-row">
          @if (rankLabel) {
            <div class="rank-chip">{{ rankLabel(article, i) }}</div>
          }
          <news-card
            class="article-row-card"
            [article]="article"
            [sourceName]="sourceNameOf(article.sourceId)"
            [translationEnabled]="translationEnabled()"
          ></news-card>
        </div>
      } @empty {
        <div class="empty-state">
          <div class="icon" [innerHTML]="icon"></div>
          <p>{{ i18n.t(emptyKey) }}</p>
        </div>
      }

      @if (total() > 0) {
        <div class="pagination">
          <button [disabled]="page() === 1" (click)="goTo(page() - 1)">&#8592; {{ i18n.t('common.prev') }}</button>
          <span>{{ i18n.t('common.pageOf', { page: page(), total: totalPages() }) }}</span>
          <button [disabled]="page() >= totalPages()" (click)="goTo(page() + 1)">{{ i18n.t('common.next') }} &#8594;</button>
          <span class="text-sm text-secondary">{{ i18n.t('common.totalCount', { total: total() }) }}</span>
        </div>
      }
    }
  `,
})
export class ArticleBoardComponent implements OnInit {
  // ---- fixed criteria (scalars: see the class comment) ----
  @Input() categorySlug = '';
  @Input() categoryPrefix = '';
  @Input() sourceId = 0;
  @Input() followOnly = false;
  @Input() defaultSort: SortBy = 'time';

  /** Empty-state message key. */
  @Input() emptyKey = 'empty.recommend';
  /** Empty-state glyph (an HTML entity). */
  @Input() icon = '&#128270;';
  @Input() pageSize = 20;
  /** Optional rank/score badge, used by the hot-rank board. */
  @Input() rankLabel: ((article: Article, index: number) => string) | null = null;
  /** Supplies the data. Required. */
  @Input() fetcher!: ArticleFetcher;

  readonly articles = signal<Article[]>([]);
  readonly total = signal(0);
  readonly page = signal(1);
  readonly loading = signal(false);
  readonly translationEnabled = signal(true);

  /** sourceId -> source name, for the card badge fallback. */
  private readonly sourceNames = signal<Record<number, string>>({});

  /** The last filter emitted by the filter bar (dates, keyword, sort). */
  private filters: Partial<NewsQuery> = {};

  constructor(
    private newsService: NewsService,
    private dataSourceService: DataSourceService,
    public i18n: I18nService,
    private translationService: TranslationService
  ) {}

  ngOnInit(): void {
    void this.init();
  }

  private async init(): Promise<void> {
    // Whether to offer "translate now" depends on the backend configuration;
    // asking once per board is enough and keeps the card free of the query.
    try {
      const status = await this.translationService.getStatus();
      this.translationEnabled.set(status.configured && status.reachable);
    } catch {
      this.translationEnabled.set(false);
    }
    try {
      const sources = await this.dataSourceService.list();
      const map: Record<number, string> = {};
      for (const s of sources) map[s.id] = s.name;
      this.sourceNames.set(map);
    } catch {
      // Without source names the badge simply falls back to "unknown".
    }
    await this.load();
  }

  /** Badge fallback: the name of the source an article came from. */
  sourceNameOf(sourceId: number): string {
    return this.sourceNames()[sourceId] || '';
  }

  /**
   * A changed criterion (the user clicked a category card) restarts the query
   * from page 1. Scalar inputs keep this from firing spuriously.
   */
  ngOnChanges(changes: SimpleChanges): void {
    const relevant = ['categorySlug', 'categoryPrefix', 'sourceId', 'followOnly', 'fetcher'];
    const changed = relevant.some((k) => changes[k] && !changes[k].firstChange);
    if (changed) {
      this.page.set(1);
      void this.load();
    }
  }

  /** The filter bar changed: restart from page 1. */
  async onQuery(q: NewsQuery): Promise<void> {
    const { page, pageSize, ...rest } = q;
    this.filters = rest;
    this.page.set(1);
    await this.load();
  }

  async goTo(page: number): Promise<void> {
    if (page < 1 || page > this.totalPages()) return;
    this.page.set(page);
    await this.load();
  }

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      const result = await this.fetcher(this.currentQuery());
      this.articles.set(result.articles);
      this.total.set(result.total);
    } catch (err) {
      console.error('[article-board] load failed:', err);
      this.articles.set([]);
      this.total.set(0);
    } finally {
      this.loading.set(false);
    }
  }

  /** The exact query that would be sent. */
  currentQuery(): NewsQuery {
    return defaultQuery({
      ...this.filters,
      categorySlug: this.categorySlug,
      categoryPrefix: this.categoryPrefix,
      sourceId: this.sourceId || undefined,
      followOnly: this.followOnly,
      page: this.page(),
      pageSize: this.pageSize,
    });
  }

  totalPages(): number {
    return Math.max(1, Math.ceil(this.total() / this.pageSize));
  }

  /** Re-runs the current query; used after an article or the profile changes. */
  async refresh(): Promise<void> {
    await this.load();
  }
}
