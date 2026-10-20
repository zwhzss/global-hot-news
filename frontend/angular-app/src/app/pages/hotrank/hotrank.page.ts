import { Component, OnDestroy, OnInit, ViewChild } from '@angular/core';
import { I18nService } from '../../services/i18n.service';
import { Article, NewsQuery } from '../../services/news.service';
import { HotRankItem, HotRankService } from '../../services/hotrank.service';
import { PlaybackService } from '../../services/playback.service';
import { ArticleBoardComponent } from '../../components/article-board/article-board.component';

/**
 * Hot-rank board.
 *
 * Supports the same date/title filters as every other board, which is why the
 * backend recomputes the rank inside the filtered result instead of replaying a
 * stored snapshot: after filtering, "stored #37 shown first" would be nonsense.
 */
@Component({
  selector: 'app-hotrank',
  standalone: false,
  template: `
    <div class="page-header flex flex-between">
      <h2>{{ i18n.t('nav.hotrank') }}</h2>
      <div class="flex" style="gap: 8px; align-items: center;">
        <button class="btn" (click)="playback.playRound()">{{ i18n.t('player.play') }}</button>
        <button class="btn" (click)="playback.pause()">{{ i18n.t('player.pause') }}</button>
        <button class="btn" (click)="playback.stop()">{{ i18n.t('player.stop') }}</button>
        <button class="btn" (click)="recalculate()">{{ i18n.t('hotrank.recalculate') }}</button>
      </div>
    </div>
    <div class="page-content">
      <article-board
        defaultSort="hot"
        [fetcher]="fetcher"
        [rankLabel]="rankLabel"
        emptyKey="empty.hotrank"
        icon="&#9650;"
      ></article-board>
    </div>
  `,
})
export class HotRankPage implements OnInit, OnDestroy {
  @ViewChild(ArticleBoardComponent) board?: ArticleBoardComponent;

  private scores = new Map<number, number>();

  readonly fetcher = async (q: NewsQuery) => {
    const { items, total } = await this.hotrankService.search(q);
    this.scores.clear();
    for (const item of items) {
      this.scores.set(item.hotRank.articleId, item.hotRank.score);
    }
    return { articles: items.map((i: HotRankItem) => i.article), total };
  };

  /**
   * Rank comes from the position in the filtered list (the backend numbers the
   * page it returns), the score from the snapshot.
   */
  readonly rankLabel = (article: Article, index: number): string => {
    const offset = (this.board ? (this.board.page() - 1) * this.board.pageSize : 0) + index + 1;
    const score = this.scores.get(article.id);
    return score === undefined ? `#${offset}` : `#${offset} · ${score.toFixed(2)}`;
  };

  constructor(
    public i18n: I18nService,
    private hotrankService: HotRankService,
    public playback: PlaybackService
  ) {}

  ngOnInit() {
    // Auto-play lives on this board: watching the round here starts the
    // playlist as soon as synthesis for the current round finishes.
    this.playback.startWatch();
  }

  ngOnDestroy() {
    this.playback.stopWatch();
  }

  async recalculate() {
    await this.hotrankService.recalculate();
    await this.board?.refresh();
  }
}
