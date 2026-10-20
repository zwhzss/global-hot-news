import { Injectable } from '@angular/core';
import { HotRankGetTop, HotRankRecalculate, HotRankSearch } from '../../../wailsjs/go/bindings/Bindings';
import { Article, NewsQuery, toArticle } from './news.service';

export interface HotRankItem {
  hotRank: {
    id: number;
    articleId: number;
    rank: number;
    score: number;
    sourceId: number;
    fetchedAt: string;
  };
  article: Article;
}

function toHotRankItem(item: any): HotRankItem {
  return {
    hotRank: {
      id: item?.HotRank?.ID || 0,
      articleId: item?.HotRank?.articleId || 0,
      rank: item?.HotRank?.rank || 0,
      score: item?.HotRank?.score || 0,
      sourceId: item?.HotRank?.sourceId || 0,
      fetchedAt: item?.HotRank?.fetchedAt ? new Date(item.HotRank.fetchedAt).toISOString() : '',
    },
    article: toArticle(item.Article),
  };
}

@Injectable({ providedIn: 'root' })
export class HotRankService {
  async getTop(limit: number = 50): Promise<HotRankItem[]> {
    const result = await HotRankGetTop(limit);
    return (result || []).map(toHotRankItem);
  }

  /**
   * Hot-ranked articles honouring the shared filter request.
   *
   * Returns the rank entries rather than plain articles so the board can show
   * the score; the backend recomputes `rank` as the position inside the FILTERED
   * result, so the numbering is always a coherent 1..N.
   */
  async search(query: NewsQuery): Promise<{ items: HotRankItem[]; total: number }> {
    const result: any = await HotRankSearch(query as any);
    const items = (result?.ranks || []).map(toHotRankItem);
    return { items, total: result?.total ?? items.length };
  }

  async recalculate(): Promise<void> {
    await HotRankRecalculate();
  }
}
