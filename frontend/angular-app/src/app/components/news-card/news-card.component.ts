import { Component, Input, OnInit, computed, signal } from '@angular/core';
import { Article, articleVariant, categoryLabel } from '../../services/news.service';
import { FollowService } from '../../services/follow.service';
import { I18nService, Lang } from '../../services/i18n.service';
import { TranslationService } from '../../services/translation.service';
import { PlaybackService } from '../../services/playback.service';

/**
 * One article card.
 *
 * The article itself is bilingual: `title`/`summary` hold the original text and
 * the Zh/En columns hold both variants. The tabs switch which variant is shown,
 * so a Chinese headline can be read in English and vice versa without leaving
 * the board.
 *
 * TTS controls: each card shows play/pause when its audio is ready, or a
 * "synthesize" button when audio is missing. The shared PlaybackService owns
 * the single <audio> element, so clicking play on one card stops any other
 * article that was playing.
 */
@Component({
  selector: 'news-card',
  standalone: false,
  template: `
    <div class="news-card" (click)="openArticle()">
      <div class="news-card-head">
        <div class="lang-tabs" (click)="$event.stopPropagation()">
          @for (l of langs; track l) {
            <button
              class="lang-tab"
              [class.active]="effectiveLang() === l"
              [title]="l === 'zh' ? i18n.t('content.zh') : i18n.t('content.en')"
              (click)="setLang(l)"
            >{{ l === 'zh' ? i18n.t('content.zh') : i18n.t('content.en') }}</button>
          }
          @if (variant().isTranslation) {
            <span class="lang-badge">{{ i18n.t('content.translation') }}</span>
          } @else if (!variant().missing) {
            <span class="lang-badge original">{{ i18n.t('content.original') }}</span>
          }
        </div>
      </div>

      <div class="news-card-title">
        <a href="{{ article.originUrl }}" target="_blank" (click)="$event.stopPropagation()">{{ variant().title }}</a>
      </div>
      @if (categoryLabel(); as cat) {
        <div class="news-card-category">
          {{ cat }}
          @if (article.classifiedBy === 'llm') {
            <span class="llm-badge" [title]="i18n.t('card.llmClassified')">AI</span>
          }
        </div>
      }

      @if (variant().lead) {
        <div class="news-card-lead">{{ variant().lead }}</div>
      }
      @if (variant().summary) {
        <div class="news-card-summary">{{ variant().summary }}</div>
      }

      @if (variant().missing) {
        <div class="trans-hint" (click)="$event.stopPropagation()">
          <span>{{ i18n.t('content.pending') }}</span>
          @if (translationOff()) {
            <span class="text-secondary">· {{ i18n.t('content.unavailable') }}</span>
          } @else {
            <button class="btn btn-sm" [disabled]="translating()" (click)="translateNow()">
              {{ translating() ? i18n.t('content.translating') : i18n.t('content.translateNow') }}
            </button>
          }
          @if (translateError()) {
            <span class="text-error">{{ translateError() }}</span>
          }
        </div>
      }

      <div class="news-card-meta flex flex-between">
        <div class="gap-8 flex">
          <span class="source">{{ badgeLabel() }}</span>
          <span class="time">{{ formatDate(article.fetchedAt) }}</span>
        </div>
        <button class="btn btn-sm" (click)="$event.stopPropagation(); toggleFollow()">
          {{ isFollowing() ? i18n.t('card.unfollow') : i18n.t('card.follow') }}
        </button>
      </div>

      <!-- TTS controls: shown below the meta line -->
      <div class="news-card-tts" (click)="$event.stopPropagation()">
        @if (articleStatus().hasAudio) {
          @if (isArticlePlaying()) {
            <button class="tts-btn tts-btn-playing" (click)="pauseArticle()" title="{{ i18n.t('player.pause') }}">
              &#9208;
            </button>
          } @else {
            <button class="tts-btn tts-btn-play" (click)="playArticle()" title="{{ i18n.t('player.play') }}">
              &#9654;
            </button>
          }
        } @else if (articleStatus().synthesizing) {
          <span class="tts-btn tts-btn-synthesizing" title="{{ i18n.t('player.synthesizing') }}">
            &#8987; {{ i18n.t('player.synthesizing') }}
          </span>
        } @else {
          <button
            class="tts-btn tts-btn-synthesize"
            [disabled]="synthesizing()"
            (click)="synthesizeArticle()"
            title="{{ i18n.t('tts.synthesizeNow') }}"
          >
            {{ synthesizing() ? i18n.t('tts.synthesizing') : i18n.t('tts.synthesizeNow') }}
          </button>
        }
      </div>
    </div>
  `,
})
export class NewsCardComponent implements OnInit {
  @Input() article!: Article;
  /** Data source name, used as the badge when the article has no category. */
  @Input() sourceName = '';
  /** Translation is configured: controls whether "translate now" is offered. */
  @Input() translationEnabled = true;

  readonly langs: Lang[] = ['zh', 'en'];

  /** null = follow the UI language; set = explicit per-card choice. */
  private readonly contentLang = signal<Lang | null>(null);

  /** Per-article TTS status: { hasAudio, synthesizing }. */
  readonly articleStatus = signal<{ hasAudio: boolean; synthesizing: boolean }>({
    hasAudio: false,
    synthesizing: false,
  });
  /** Whether this card's article is the one currently playing. */
  readonly isArticlePlaying = computed(() => {
    const cur = this.playback.current();
    return cur !== null && cur.articleId === this.article.id && this.playback.playing();
  });
  readonly synthesizing = signal(false);

  /** Local copy so an on-demand translation can refresh the card in place. */
  private readonly current = signal<Article | null>(null);

  readonly isFollowing = signal(false);
  readonly translating = signal(false);
  readonly translateError = signal('');

  readonly effectiveLang = computed<Lang>(() => this.contentLang() ?? this.i18n.lang());
  readonly variant = computed(() => articleVariant(this.articleForDisplay(), this.effectiveLang()));
  readonly translationOff = computed(() => !this.translationEnabled);

  constructor(
    private followService: FollowService,
    public i18n: I18nService,
    private translationService: TranslationService,
    public playback: PlaybackService
  ) {}

  async ngOnInit() {
    this.current.set(this.article);
    this.isFollowing.set(await this.followService.isArticleFollowed(this.article.id));
    await this.refreshStatus();
  }

  private articleForDisplay(): Article {
    return this.current() ?? this.article;
  }

  setLang(lang: Lang) {
    this.contentLang.set(lang);
  }

  async toggleFollow() {
    const next = !this.isFollowing();
    try {
      if (next) {
        await this.followService.followArticle(this.article.id);
      } else {
        await this.followService.unfollowArticle(this.article.id);
      }
      this.isFollowing.set(next);
    } catch (err) {
      console.error('[news-card] toggleFollow failed:', err);
      this.isFollowing.set(!next);
    }
  }

  /** Generates the missing translation for this one article, on demand. */
  async translateNow() {
    this.translating.set(true);
    this.translateError.set('');
    try {
      const updated = await this.translationService.translateArticle(this.article.id);
      this.current.set(updated);
    } catch (err: any) {
      this.translateError.set(String(err?.message || err));
    } finally {
      this.translating.set(false);
    }
  }

  openArticle() {
    if (this.article.originUrl) {
      window.open(this.article.originUrl, '_blank');
    }
  }

  /**
   * The badge shows where an article came from: its own publisher when the
   * feed provided one, otherwise the data source name. Provenance is always
   * shown — a category label says what a story is about, not where it came
   * from, and mixing the two hid the source for every classified article.
   */
  badgeLabel(): string {
    return this.article.publisher || this.sourceName || this.i18n.t('card.unknown');
  }

  /** The article's first category, when keyword classification matched one. */
  categoryLabel(): string {
    const category = this.article.categories?.[0];
    return category ? categoryLabel(category, this.i18n.lang()) : '';
  }

  formatDate(dateStr: string): string {
    if (!dateStr) return '';
    const d = new Date(dateStr);
    const diff = Date.now() - d.getTime();
    const minutes = Math.floor(diff / 60000);
    if (minutes < 1) return this.i18n.t('card.justNow');
    if (minutes < 60) return this.i18n.t('card.minutesAgo', { n: minutes });
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return this.i18n.t('card.hoursAgo', { n: hours });
    const days = Math.floor(hours / 24);
    if (days < 7) return this.i18n.t('card.daysAgo', { n: days });
    return d.toLocaleDateString(this.i18n.lang() === 'zh' ? 'zh-CN' : 'en-US');
  }

  // ---- TTS per-article controls ----

  async refreshStatus() {
    const s = await this.playback.getArticleStatus(this.article.id);
    this.articleStatus.set(s);
  }

  async playArticle() {
    const played = await this.playback.playArticle(this.article.id);
    if (played) {
      await this.refreshStatus();
    }
  }

  pauseArticle() {
    this.playback.pause();
    // Status will refresh on next tick from the round watcher, or on next interaction.
  }

  async synthesizeArticle() {
    this.synthesizing.set(true);
    try {
      await this.playback.synthesizeArticle(this.article.id);
      await this.refreshStatus();
    } catch (e) {
      console.error('[news-card] synthesizeArticle failed:', e);
    } finally {
      this.synthesizing.set(false);
    }
  }
}
