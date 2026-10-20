import { Injectable, signal } from '@angular/core';
import {
  FollowArticle,
  FollowSource,
  FollowedSearch,
  GetFollowedSources,
  IsArticleFollowed,
  IsSourceFollowed,
  RecordBehavior,
  UnfollowArticle,
  UnfollowSource,
} from '../../../wailsjs/go/bindings/Bindings';
import { models } from '../../../wailsjs/go/models';
import { Article, NewsQuery, toArticle } from './news.service';
import { Source } from './datasource.service';

function toSource(s: models.Source): Source {
  return {
    id: s.ID,
    name: s.name,
    type: s.type,
    url: s.url,
    isActive: s.isActive,
    cronExpression: s.cronExpression,
    requestInterval: s.requestInterval,
    publisher: s.publisher || '',
    status: s.status,
    lastError: s.lastError,
    lastFetchedAt: s.lastFetchedAt ? new Date(s.lastFetchedAt).toISOString() : undefined,
    fetchCount: s.fetchCount,
    createdAt: s.createdAt ? new Date(s.createdAt).toISOString() : '',
    updatedAt: s.updatedAt ? new Date(s.updatedAt).toISOString() : '',
  };
}

@Injectable({ providedIn: 'root' })
export class FollowService {
  private followedArticles = signal<Article[]>([]);
  private followedSources = signal<Source[]>([]);
  followedArticles$ = this.followedArticles.asReadonly();
  followedSources$ = this.followedSources.asReadonly();

  async followArticle(articleId: number): Promise<void> {
    await FollowArticle(articleId);
  }

  async unfollowArticle(articleId: number): Promise<void> {
    await UnfollowArticle(articleId);
  }

  async followSource(sourceId: number): Promise<void> {
    await FollowSource(sourceId);
  }

  async unfollowSource(sourceId: number): Promise<void> {
    await UnfollowSource(sourceId);
  }

  async isArticleFollowed(articleId: number): Promise<boolean> {
    return await IsArticleFollowed(articleId);
  }

  async isSourceFollowed(sourceId: number): Promise<boolean> {
    return await IsSourceFollowed(sourceId);
  }

  /**
   * Followed articles for the given filter request.
   *
   * Uses the shared toArticle mapper: this service used to have its own copy
   * that dropped `categories` and the bilingual fields, so followed articles
   * rendered differently from the same article on any other board.
   */
  async search(query: NewsQuery): Promise<{ articles: Article[]; total: number }> {
    const result = await FollowedSearch(query as any);
    const articles = (result?.articles || []).map(toArticle);
    this.followedArticles.set(articles);
    return { articles, total: result?.total ?? articles.length };
  }

  async getFollowedSources(): Promise<Source[]> {
    const result = await GetFollowedSources();
    const sources = (result || []).map(toSource);
    this.followedSources.set(sources);
    return sources;
  }

  async recordBehavior(articleId: number, sourceId: number, stayMs: number, clickedUrl: boolean): Promise<void> {
    await RecordBehavior(articleId, sourceId, stayMs, clickedUrl);
  }
}
