import { Injectable, signal } from '@angular/core';
import { RecommendGetProfile, RecommendResetProfile, RecommendSearch } from '../../../wailsjs/go/bindings/Bindings';
import { Article, NewsQuery, toArticle } from './news.service';

export interface ProfileData {
  categoryWeights: Record<string, number>;
  sourceWeights: Record<string, number>;
  behaviorCount?: number;
}

@Injectable({ providedIn: 'root' })
export class RecommendService {
  private recommendations = signal<Article[]>([]);
  recommendations$ = this.recommendations.asReadonly();

  /** Recommendations for the given filter request. */
  async search(query: NewsQuery): Promise<{ articles: Article[]; total: number }> {
    const result = await RecommendSearch(query as any);
    const articles = (result?.articles || []).map(toArticle);
    this.recommendations.set(articles);
    return { articles, total: result?.total ?? articles.length };
  }

  async getProfile(): Promise<ProfileData> {
    const raw = await RecommendGetProfile();
    try {
      return JSON.parse(raw || '{}');
    } catch {
      return { categoryWeights: {}, sourceWeights: {} };
    }
  }

  async resetProfile(): Promise<void> {
    await RecommendResetProfile();
  }
}
