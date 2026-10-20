import { Component, EventEmitter, Input, OnInit, Output, signal } from '@angular/core';
import { DatePreset, NewsQuery, NewsService, SortBy, defaultQuery } from '../../services/news.service';
import { I18nService } from '../../services/i18n.service';

/**
 * The shared filter bar, used by every board.
 *
 * Defaults to "today, newest first" because the article table only grows: a
 * board that always opened on the full history became unusable over time. The
 * date control, the fuzzy title search and the sort order are the additional
 * filters the requirement asks for.
 *
 * The component only emits the *filter* part of a NewsQuery (dates, keyword,
 * sort); each board merges in its own fixed criteria (category, source,
 * followOnly) and its pagination. That keeps the board the single owner of its
 * data source.
 *
 * All inputs are scalars: object inputs are a new reference on every change
 * detection pass, which would make any ngOnChanges-driven reload loop.
 */
@Component({
  selector: 'news-filter',
  standalone: false,
  template: `
    <div class="filter-bar">
      <div class="filter-row">
        <span class="filter-label">{{ i18n.t('filter.date') }}</span>
        <div class="chips">
          @for (p of presets; track p.value) {
            <button
              class="chip"
              [class.active]="preset() === p.value"
              (click)="setPreset(p.value)"
            >{{ i18n.t(p.labelKey) }}</button>
          }
        </div>

        @if (preset() === 'custom') {
          <span class="filter-custom">
            <input type="date" [value]="from()" (change)="setFrom($event)" />
            <span class="filter-label">{{ i18n.t('filter.to') }}</span>
            <input type="date" [value]="to()" (change)="setTo($event)" />
          </span>
        }

        @if (dates().length > 0) {
          <select class="date-select" [value]="''" (change)="pickDate($event)" [title]="i18n.t('filter.availableDates')">
            <option value="">{{ i18n.t('filter.availableDates') }}</option>
            @for (d of dates(); track d) {
              <option [value]="d">{{ d }}</option>
            }
          </select>
        }
      </div>

      <div class="filter-row">
        <span class="filter-label">{{ i18n.t('filter.title') }}</span>
        <input
          class="filter-input"
          type="text"
          [value]="keyword()"
          [placeholder]="i18n.t('filter.titlePlaceholder')"
          (input)="setKeyword($event)"
          (keyup.enter)="emit()"
        />
        <span class="filter-label">{{ i18n.t('filter.sort') }}</span>
        <select class="date-select" [value]="sortBy()" (change)="setSort($event)">
          <option value="time">{{ i18n.t('filter.sortTime') }}</option>
          <option value="hot">{{ i18n.t('filter.sortHot') }}</option>
        </select>
        <button class="btn btn-primary btn-sm" (click)="emit()">{{ i18n.t('filter.apply') }}</button>
        <button class="btn btn-sm" (click)="reset()">{{ i18n.t('common.reset') }}</button>
        <span class="filter-count text-sm text-secondary">{{ i18n.t('filter.resultCount', { total: total }) }}</span>
      </div>
    </div>
  `,
})
export class NewsFilterComponent implements OnInit {
  /** Sort order the board starts with. */
  @Input() defaultSort: SortBy = 'time';
  /** Current match count, shown next to the buttons. */
  @Input() total = 0;

  @Output() queryChange = new EventEmitter<NewsQuery>();

  readonly preset = signal<DatePreset>('today');
  readonly from = signal('');
  readonly to = signal('');
  readonly keyword = signal('');
  readonly sortBy = signal<SortBy>('time');
  readonly dates = signal<string[]>([]);

  readonly presets: { value: DatePreset; labelKey: string }[] = [
    { value: 'today', labelKey: 'filter.today' },
    { value: '7d', labelKey: 'filter.last7' },
    { value: '30d', labelKey: 'filter.last30' },
    { value: 'all', labelKey: 'filter.allDates' },
    { value: 'custom', labelKey: 'filter.custom' },
  ];

  constructor(private newsService: NewsService, public i18n: I18nService) {}

  async ngOnInit() {
    this.sortBy.set(this.defaultSort || 'time');
    // Only fetch the date list if the backend is reachable; a failure simply
    // hides the dropdown.
    this.dates.set(await this.newsService.availableDates(90));
  }

  /** Emits the current filter selection. */
  emit(): void {
    this.queryChange.emit(defaultQuery(this.build()));
  }

  private build(): Partial<NewsQuery> {
    const preset = this.preset();
    return {
      datePreset: preset,
      dateFrom: preset === 'custom' ? this.from() : undefined,
      dateTo: preset === 'custom' ? this.to() : undefined,
      titleKeyword: this.keyword(),
      sortBy: this.sortBy(),
    };
  }

  setPreset(p: DatePreset) {
    this.preset.set(p);
    if (p !== 'custom') {
      this.emit();
    }
  }

  setFrom(event: Event) {
    this.from.set((event.target as HTMLInputElement).value);
    if (this.from() && this.to()) this.emit();
  }

  setTo(event: Event) {
    this.to.set((event.target as HTMLInputElement).value);
    if (this.from() && this.to()) this.emit();
  }

  /** Jump straight to a date that is known to contain articles. */
  pickDate(event: Event) {
    const value = (event.target as HTMLSelectElement).value;
    if (!value) return;
    this.preset.set('custom');
    this.from.set(value);
    this.to.set(value);
    this.emit();
  }

  setKeyword(event: Event) {
    this.keyword.set((event.target as HTMLInputElement).value);
  }

  setSort(event: Event) {
    this.sortBy.set((event.target as HTMLSelectElement).value as SortBy);
    this.emit();
  }

  reset() {
    this.preset.set('today');
    this.from.set('');
    this.to.set('');
    this.keyword.set('');
    this.sortBy.set(this.defaultSort || 'time');
    this.emit();
  }
}
