import { Component, OnDestroy, OnInit, ViewChild } from '@angular/core';
import { I18nService } from '../../services/i18n.service';
import { NewsQuery } from '../../services/news.service';
import { RecommendService } from '../../services/recommend.service';
import { PlaybackService } from '../../services/playback.service';
import { ArticleBoardComponent, ArticleFetcher } from '../../components/article-board/article-board.component';

/**
 * Recommendation board.
 *
 * Cold start shows the hot list; once browsing behaviour exists the score is
 * personalised. Either way the same filter bar applies: the recommendation
 * service scores a bounded, already-filtered candidate set.
 */
@Component({
  selector: 'app-recommend',
  standalone: false,
  template: `
    <div class="page-header flex flex-between">
      <h2>{{ i18n.t('nav.recommend') }}</h2>
      <div class="flex" style="gap: 8px; align-items: center;">
        <button class="btn" (click)="playback.playRound()">{{ i18n.t('player.play') }}</button>
        <button class="btn" (click)="playback.pause()">{{ i18n.t('player.pause') }}</button>
        <button class="btn" (click)="playback.stop()">{{ i18n.t('player.stop') }}</button>
        <button class="btn" (click)="resetProfile()">{{ i18n.t('recommend.resetProfile') }}</button>
      </div>
    </div>
    <div class="page-content">
      <article-board
        defaultSort="hot"
        [fetcher]="fetcher"
        emptyKey="empty.recommend"
        icon="&#128270;"
      ></article-board>
    </div>
  `,
})
export class RecommendPage implements OnInit, OnDestroy {
  @ViewChild(ArticleBoardComponent) board?: ArticleBoardComponent;

  readonly fetcher: ArticleFetcher = (q: NewsQuery) => this.recommendService.search(q);

  constructor(
    public i18n: I18nService,
    private recommendService: RecommendService,
    public playback: PlaybackService
  ) {}

  ngOnInit() {
    // Auto-play lives on this board too (需求: 热榜和推荐文章自动播放).
    this.playback.startWatch();
  }

  ngOnDestroy() {
    this.playback.stopWatch();
  }

  async resetProfile() {
    if (!confirm(this.i18n.t('recommend.resetConfirm'))) return;
    await this.recommendService.resetProfile();
    await this.board?.refresh();
  }
}
