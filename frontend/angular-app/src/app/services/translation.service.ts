import { Injectable, signal } from '@angular/core';
import {
  ClassifierAudit,
  ClassifierAuditReclassify,
  ClassifierAuditUnclassified,
  ClassifierCancel,
  ClassifierCounts,
  ClassifierTest,
  LLMCancelAll,
  LLMQueueStatus,
  TranslationCancel,
  TranslationDetectLocal,
  TranslationListModels,
  TranslationResetFailed,
  TranslationRunNow,
  TranslationStats,
  TranslationStatus,
  TranslationTest,
  TranslationTranslateArticle,
} from '../../../wailsjs/go/bindings/Bindings';
import { models } from '../../../wailsjs/go/models';
import { Article, toArticle } from './news.service';

export interface AuditItem {
  articleId: number;
  title: string;
  categoryId: number;
  category: string;
  keyword: string;
  hitCount: number;
}

export interface AuditCategoryStat {
  categoryId: number;
  category: string;
  total: number;
  weak: number;
}

export interface AuditCounts {
  total: number;
  unclassified: number;
}

export interface AuditResult {
  categories: AuditCategoryStat[];
  suspects: AuditItem[];
  counts: AuditCounts;
}

/** One job on the shared LLM queue. */
export interface QueueJob {
  id: number;
  kind: string; // 'translate' | 'classify'
  name: string;
  startedAt: string;
  item?: string; // title of the article being worked on right now
  done?: number; // articles finished so far in this job
  total?: number; // articles planned for this job
}

/** Snapshot of the shared LLM queue: one job at a time, FIFO, cancellable. */
export interface QueueStatus {
  queued: number;
  current: QueueJob | null;
}

export interface TranslationStats {
  total: number;
  done: number;
  pending: number;
  processable: number;
  failed: number;
  skipped: number;
  cacheHit: number;
}

export interface TranslationStatus {
  configured: boolean;
  engine: string;
  provider: string;
  model: string;
  baseUrl: string;
  local: boolean;
  scope: string;
  dailyLimit: number;
  translatedToday: number;
  reachable: boolean;
  isRunning: boolean;
  lastRun: string;
  lastError: string;
  lastCount: number;
}

export interface LocalModel {
  name: string;
  baseUrl: string;
  models: string[];
}

/**
 * Thin wrapper over the translation bindings.
 *
 * Kept separate from NewsService so the article card can ask for one on-demand
 * translation without pulling in the whole list machinery.
 */
@Injectable({ providedIn: 'root' })
export class TranslationService {
  private readonly statusSignal = signal<TranslationStatus | null>(null);
  private readonly statsSignal = signal<TranslationStats | null>(null);
  readonly status$ = this.statusSignal.asReadonly();
  readonly stats$ = this.statsSignal.asReadonly();

  async getStatus(): Promise<TranslationStatus> {
    const raw: any = await TranslationStatus();
    const status: TranslationStatus = {
      configured: !!raw?.configured,
      engine: raw?.engine || 'llm',
      provider: raw?.provider || '',
      model: raw?.model || '',
      baseUrl: raw?.baseUrl || '',
      local: !!raw?.local,
      scope: raw?.scope || 'title_summary',
      dailyLimit: raw?.dailyLimit ?? 0,
      translatedToday: raw?.translatedToday ?? 0,
      reachable: !!raw?.reachable,
      isRunning: !!raw?.isRunning,
      lastRun: raw?.lastRun ? new Date(raw.lastRun).toISOString() : '',
      lastError: raw?.lastError || '',
      lastCount: raw?.lastCount ?? 0,
    };
    this.statusSignal.set(status);
    return status;
  }

  async getStats(): Promise<TranslationStats> {
    const raw: any = await TranslationStats();
    const stats: TranslationStats = {
      total: raw?.total ?? 0,
      done: raw?.done ?? 0,
      pending: raw?.pending ?? 0,
      processable: raw?.processable ?? 0,
      failed: raw?.failed ?? 0,
      skipped: raw?.skipped ?? 0,
      cacheHit: raw?.cacheHit ?? 0,
    };
    this.statsSignal.set(stats);
    return stats;
  }

  /**
   * Queues a translation pass over the pending articles. Returns whether a job
   * was queued (false = a pass is already queued or running); progress is
   * observed through getStatus()/getStats() and the queue status.
   */
  async runNow(limit: number = 10): Promise<boolean> {
    return (await TranslationRunNow(limit)) ?? false;
  }

  /** Cancels queued and running translation jobs; returns how many were affected. */
  async cancelTranslate(): Promise<number> {
    return (await TranslationCancel()) ?? 0;
  }

  /** Cancels queued and running classification jobs. */
  async cancelClassify(): Promise<number> {
    return (await ClassifierCancel()) ?? 0;
  }

  /** Stops every queued and running model task. */
  async cancelAll(): Promise<number> {
    return (await LLMCancelAll()) ?? 0;
  }

  /** Queue depth + the currently running job; safe to poll. */
  async queueStatus(): Promise<QueueStatus> {
    const raw: any = await LLMQueueStatus();
    return {
      queued: raw?.queued ?? 0,
      current: raw?.current
        ? {
            id: raw.current.id ?? 0,
            kind: raw.current.kind || '',
            name: raw.current.name || '',
            startedAt: raw.current.startedAt || '',
            item: raw.current.item || '',
            done: raw.current.done ?? 0,
            total: raw.current.total ?? 0,
          }
        : null,
    };
  }

  /** One real translation, to prove the configuration works. */
  async test(): Promise<string> {
    return await TranslationTest();
  }

  async detectLocalModels(): Promise<LocalModel[]> {
    const raw: any[] = (await TranslationDetectLocal()) || [];
    return raw.map((m) => ({
      name: m?.name || '',
      baseUrl: m?.baseUrl || '',
      models: m?.models || [],
    }));
  }

  async listModels(): Promise<string[]> {
    return (await TranslationListModels()) || [];
  }

  async resetFailed(): Promise<number> {
    return (await TranslationResetFailed()) ?? 0;
  }

  /** Translates one article and returns the updated row. */
  async translateArticle(id: number): Promise<Article> {
    const raw: models.Article = await TranslationTranslateArticle(id);
    return toArticle(raw);
  }

  /** Runs the classification audit and returns per-category stats + suspects. */
  async auditCategories(): Promise<AuditResult> {
    const raw: any = await ClassifierAudit();
    return {
      categories: raw?.categories || [],
      suspects: raw?.suspects || [],
      counts: normalizeCounts(raw?.counts),
    };
  }

  /** Cheap counters for the audit buttons; safe to poll. */
  async auditCounts(): Promise<AuditCounts> {
    return normalizeCounts(await ClassifierCounts());
  }

  /**
   * Queues classifying articles with no category. Returns whether a job was
   * queued (false = one is already queued or running).
   */
  async auditClassifyUnclassified(limit = 500): Promise<boolean> {
    return (await ClassifierAuditUnclassified(limit)) ?? false;
  }

  /** Queues re-classifying suspect articles. */
  async auditReclassifySuspects(limit = 500): Promise<boolean> {
    return (await ClassifierAuditReclassify(limit)) ?? false;
  }

  /** One tiny chat call that proves the classifier's cls_* endpoint works. */
  async testClassifier(): Promise<string> {
    return await ClassifierTest();
  }
}

function normalizeCounts(raw: any): AuditCounts {
  return {
    total: raw?.total ?? 0,
    unclassified: raw?.unclassified ?? 0,
  };
}
