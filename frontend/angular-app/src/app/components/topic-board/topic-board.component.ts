import { Component, Input, OnInit, computed, signal } from '@angular/core';
import {
  Category,
  NewsQuery,
  NewsService,
  categoryDescription,
  categoryLabel,
} from '../../services/news.service';
import { I18nService } from '../../services/i18n.service';
import { ArticleBoardComponent, ArticleFetcher } from '../article-board/article-board.component';

/**
 * A topic board (Finance / Military / Technology / Politics / Economy).
 *
 * The five boards differ only by slug prefix, title and empty-state text, so
 * they share one implementation and the routed pages stay one-liners. Five
 * near-identical copies is what let their behaviour drift apart before.
 *
 * The category selection is passed to the board as two scalar inputs rather
 * than one object, so selecting a card triggers exactly one reload.
 */
@Component({
  selector: 'topic-board',
  standalone: false,
  template: `
    @if (subCategories().length > 0) {
      <div class="category-grid">
        <div class="category-card" [class.active]="selected() === ''" (click)="select('')">
          <h3>{{ i18n.t('common.all') }}</h3>
          <p>{{ i18n.t('common.showingToday') }}</p>
        </div>
        @for (cat of subCategories(); track cat.slug) {
          <div class="category-card" [class.active]="selected() === cat.slug" (click)="select(cat.slug)">
            <h3>{{ label(cat) }}</h3>
            <p>{{ description(cat) }}</p>
          </div>
        }
      </div>
    }

    <article-board
      [categorySlug]="categorySlug()"
      [categoryPrefix]="categoryPrefix()"
      [fetcher]="fetcher"
      [emptyKey]="emptyKey"
      [icon]="icon"
    ></article-board>
  `,
})
export class TopicBoardComponent implements OnInit {
  /** Slug prefix that identifies the module, e.g. "finance". */
  @Input() prefix = '';
  @Input() emptyKey = 'empty.finance';
  @Input() icon = '&#36;';

  readonly categories = signal<Category[]>([]);
  readonly subCategories = signal<Category[]>([]);
  /** '' means the whole module. */
  readonly selected = signal('');

  /** Exactly one of the two is non-empty, so the backend filter is unambiguous. */
  readonly categorySlug = computed(() => this.selected());
  readonly categoryPrefix = computed(() => (this.selected() ? '' : `${this.prefix}-`));

  readonly fetcher: ArticleFetcher = (q: NewsQuery) => this.newsService.search(q);

  constructor(private newsService: NewsService, public i18n: I18nService) {}

  async ngOnInit() {
    const all = await this.newsService.listCategories();
    this.categories.set(all);
    this.subCategories.set(all.filter((c) => c.slug.startsWith(`${this.prefix}-`)));
  }

  select(slug: string) {
    this.selected.set(slug);
  }

  label(cat: Category): string {
    return categoryLabel(cat, this.i18n.lang());
  }

  description(cat: Category): string {
    return categoryDescription(cat, this.i18n.lang());
  }
}
