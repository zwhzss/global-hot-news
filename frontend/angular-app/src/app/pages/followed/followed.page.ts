import { Component, OnDestroy, OnInit, signal } from '@angular/core';
import { I18nService } from '../../services/i18n.service';
import { NewsQuery } from '../../services/news.service';
import { FollowService } from '../../services/follow.service';
import { Source } from '../../services/datasource.service';
import { PlaybackService } from '../../services/playback.service';
import { ArticleBoardComponent, ArticleFetcher } from '../../components/article-board/article-board.component';

/**
 * My Follows board: followed articles and followed sources.
 *
 * The article tab carries the same filter bar as every other board; the source
 * tab keeps the plain table it always had.
 */
@Component({
  selector: 'app-followed',
  standalone: false,
  template: `
    <div class="page-header flex flex-between">
      <h2>{{ i18n.t('nav.followed') }}</h2>
      <div class="flex" style="gap: 8px; align-items: center;">
        <button class="btn" (click)="playback.playRound()">{{ i18n.t('player.play') }}</button>
        <button class="btn" (click)="playback.pause()">{{ i18n.t('player.pause') }}</button>
        <button class="btn" (click)="playback.stop()">{{ i18n.t('player.stop') }}</button>
      </div>
    </div>
    <div class="page-content">
      <div class="tabs">
        <div class="tab" [class.active]="tab() === 'articles'" (click)="tab.set('articles')">
          {{ i18n.t('followed.tabArticles') }}
        </div>
        <div class="tab" [class.active]="tab() === 'sources'" (click)="tab.set('sources')">
          {{ i18n.t('followed.tabSources') }}
        </div>
      </div>

      @if (tab() === 'articles') {
        <article-board
          [followOnly]="true"
          [fetcher]="fetcher"
          emptyKey="empty.followed"
          icon="&#9734;"
        ></article-board>
      } @else {
        @if (loadingSources()) {
          <div class="loading">{{ i18n.t('common.loading') }}</div>
        } @else {
          <table class="data-table">
            <thead>
              <tr>
                <th>{{ i18n.t('followed.name') }}</th>
                <th>{{ i18n.t('followed.type') }}</th>
                <th>{{ i18n.t('followed.status') }}</th>
                <th>{{ i18n.t('followed.url') }}</th>
                <th>{{ i18n.t('followed.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              @for (source of followedSources(); track source.id) {
                <tr>
                  <td>{{ source.name }}</td>
                  <td>{{ source.type }}</td>
                  <td><span class="status-badge status-{{ source.status }}">{{ source.status }}</span></td>
                  <td><a href="{{ source.url }}" target="_blank">{{ truncate(source.url, 40) }}</a></td>
                  <td>
                    <button class="btn btn-sm btn-danger" (click)="unfollowSource(source.id)">
                      {{ i18n.t('followed.unfollow') }}
                    </button>
                  </td>
                </tr>
              } @empty {
                <tr><td colspan="5" class="text-secondary text-center">{{ i18n.t('empty.sources') }}</td></tr>
              }
            </tbody>
          </table>
        }
      }
    </div>
  `,
})
export class FollowedPage implements OnInit, OnDestroy {
  // NOTE: zoneless change detection — state assigned after `await` must be signal-based.
  readonly tab = signal<'articles' | 'sources'>('articles');
  readonly followedSources = signal<Source[]>([]);
  readonly loadingSources = signal(false);

  /**
   * The followed-board must show every followed article regardless of publish
   * date; the shared defaultQuery() defaults to 'today' which would hide
   * followed articles from previous days. Override the date preset here so the
   * filter bar starts on "all" and the user can narrow manually if they want.
   */
  readonly fetcher: ArticleFetcher = (q: NewsQuery) =>
    this.followService.search({ ...q, datePreset: 'all' });

  constructor(
    public i18n: I18nService,
    private followService: FollowService,
    public playback: PlaybackService
  ) {}

  async ngOnInit() {
    await this.loadFollowedSources();
    this.playback.startWatch();
  }

  ngOnDestroy() {
    this.playback.stopWatch();
  }

  async loadFollowedSources() {
    this.loadingSources.set(true);
    try {
      this.followedSources.set(await this.followService.getFollowedSources());
    } finally {
      this.loadingSources.set(false);
    }
  }

  async unfollowSource(sourceId: number) {
    await this.followService.unfollowSource(sourceId);
    await this.loadFollowedSources();
  }

  truncate(str: string, len: number): string {
    return str.length > len ? str.substring(0, len) + '...' : str;
  }
}
