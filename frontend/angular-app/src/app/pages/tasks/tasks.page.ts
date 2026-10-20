import { Component, OnDestroy, OnInit, computed, signal } from '@angular/core';
import { I18nService } from '../../services/i18n.service';
import { TranslationService, QueueJob, QueueStatus } from '../../services/translation.service';
import { TaskEvent, TaskService } from '../../services/task.service';

/** Live view of the shared LLM queue plus its event feed. */
@Component({
  selector: 'app-tasks',
  standalone: false,
  template: `
    <div class="page-header">
      <h2>{{ i18n.t('nav.tasks') }}</h2>
    </div>
    <div class="page-content">
      <div class="modal" style="max-width: 860px;">
        <div class="flex flex-between" style="align-items: center; margin-bottom: 16px;">
          <h3 style="margin: 0;">{{ i18n.t('tasks.title') }}</h3>
          <button class="btn btn-sm" [disabled]="events().length === 0" (click)="clearLog()">
            {{ i18n.t('tasks.clear') }}
          </button>
        </div>

        @if (queue().current; as cur) {
          <div class="stat-card" style="margin-bottom: 16px;">
            <div class="stat-label">{{ i18n.t('tasks.current') }}</div>
            <div class="stat-value" style="font-size: 15px;">
              {{ jobLabel(cur) }} · {{ i18n.t('tasks.running') }} {{ elapsed(cur.startedAt) }}
            </div>
            @if (cur.item; as item) {
              <div class="text-sm text-secondary">{{ quoted(item) }}</div>
            }
            @if (cur.total) {
              <div class="text-sm text-secondary">{{ cur.done ?? 0 }} / {{ cur.total }}</div>
            }
            @if (queue().queued > 0) {
              <div class="text-sm text-secondary">
                {{ i18n.t('tasks.queued') }}: {{ queue().queued }}
              </div>
            }
            <div style="margin-top: 8px; display: flex; gap: 8px;">
              <button class="btn btn-sm" (click)="cancelCurrent()">{{ i18n.t('tasks.cancelJob') }}</button>
              <button class="btn btn-sm btn-danger" (click)="cancelAll()">{{ i18n.t('trans.cancelAll') }}</button>
            </div>
          </div>
        } @else {
          <p class="text-sm text-secondary" style="margin: 0 0 16px;">
            {{ i18n.t('tasks.idle') }}
            @if (queue().queued > 0) {
              · {{ i18n.t('tasks.queued') }}: {{ queue().queued }}
            }
          </p>
        }

        <h4 style="margin: 0 0 8px;">{{ i18n.t('tasks.feed') }}</h4>
        @if (displayEvents().length === 0) {
          <p class="text-sm text-secondary" style="margin: 0;">{{ i18n.t('tasks.empty') }}</p>
        } @else {
          <div
            style="max-height: 520px; overflow-y: auto; border: 1px solid var(--border-color); border-radius: 8px;"
          >
            @for (e of displayEvents(); track e.id) {
              <div
                style="display: flex; gap: 12px; padding: 6px 12px; font-size: 13px; border-bottom: 1px solid var(--border-color);"
                [style.color]="colorOf(e)"
              >
                <span style="flex-shrink: 0; opacity: 0.65; font-family: monospace;">{{ fmtTime(e.time) }}</span>
                <span>{{ describe(e) }}</span>
              </div>
            }
          </div>
        }
      </div>
    </div>
  `,
})
export class TasksPage implements OnInit, OnDestroy {
  // NOTE: zoneless change detection — state assigned after `await` must be a signal.
  readonly queue = signal<QueueStatus>({ queued: 0, current: null });
  readonly events = signal<TaskEvent[]>([]);
  private readonly now = signal(Date.now());

  /** Newest first, straight from the ascending feed. */
  readonly displayEvents = computed(() => this.events().slice().reverse());

  private poll: ReturnType<typeof setInterval> | null = null;
  private pollFast = true;

  constructor(
    public i18n: I18nService,
    private translationService: TranslationService,
    private taskService: TaskService,
  ) {}

  async ngOnInit() {
    await this.refresh();
    this.startPolling();
  }

  ngOnDestroy() {
    this.stopPolling();
  }

  private startPolling() {
    this.stopPolling();
    this.poll = setInterval(() => this.refresh(), this.pollFast ? 1000 : 3000);
  }

  private stopPolling() {
    if (this.poll) {
      clearInterval(this.poll);
      this.poll = null;
    }
  }

  async refresh() {
    this.now.set(Date.now());
    try {
      const q = await this.translationService.queueStatus();
      this.queue.set(q);
      // Track a running job second by second, idle pages at a gentler rate.
      const fast = q.current !== null;
      if (fast !== this.pollFast) {
        this.pollFast = fast;
        this.startPolling();
      }
    } catch (e) {
      console.error('Failed to load queue status:', e);
    }
    try {
      this.events.set(await this.taskService.recent(200));
    } catch (e) {
      console.error('Failed to load task events:', e);
    }
  }

  async cancelCurrent() {
    const cur = this.queue().current;
    if (!cur) return;
    try {
      if (cur.kind === 'translate') {
        await this.translationService.cancelTranslate();
      } else {
        await this.translationService.cancelClassify();
      }
    } catch (e) {
      console.error('Failed to cancel job:', e);
    }
    await this.refresh();
  }

  async cancelAll() {
    try {
      await this.translationService.cancelAll();
    } catch (e) {
      console.error('Failed to cancel jobs:', e);
    }
    await this.refresh();
  }

  async clearLog() {
    try {
      await this.taskService.clear();
    } catch (e) {
      console.error('Failed to clear task log:', e);
    }
    this.events.set([]);
  }

  jobLabel(job: QueueJob): string {
    return this.jobNameLabel(job.kind, job.name);
  }

  private jobNameLabel(kind: string, name: string): string {
    if (kind === 'translate') {
      if (name.startsWith('article:')) return this.i18n.t('trans.jobSingle');
      return name === 'batch' ? this.i18n.t('trans.jobBatch') : this.i18n.t('trans.jobSingle');
    }
    if (name === 'audit-reclassify') return this.i18n.t('trans.reclassifySuspects');
    return this.i18n.t('trans.classifyUnclassified');
  }

  /** Elapsed wall time of the running job, mm:ss. */
  elapsed(startedAt: string): string {
    const start = new Date(startedAt).getTime();
    const total = Number.isFinite(start) ? Math.max(0, Math.floor((this.now() - start) / 1000)) : 0;
    const mm = String(Math.floor(total / 60)).padStart(2, '0');
    const ss = String(total % 60).padStart(2, '0');
    return `${mm}:${ss}`;
  }

  fmtTime(time: string): string {
    const d = new Date(time);
    return Number.isNaN(d.getTime()) ? '' : d.toLocaleTimeString();
  }

  quoted(title: string): string {
    return this.i18n.isZh() ? `《${title}》` : `"${title}"`;
  }

  /** Human-readable line for one event, composed in the active language. */
  describe(e: TaskEvent): string {
    const t = (k: string) => this.i18n.t(k);
    const job = this.jobNameLabel(e.kind, e.jobName);
    const title = this.quoted(e.title || '');
    switch (e.type) {
      case 'jobQueued':
        return `${t('tasks.evtQueued')}: ${job}`;
      case 'jobStart':
        return `${t('tasks.evtStart')}: ${job}`;
      case 'jobDone':
        return `${t('tasks.evtDone')}: ${job}`;
      case 'jobCancelled':
        return `${t('tasks.evtCancelled')}: ${job}`;
      case 'jobError':
        return `${t('tasks.evtError')}: ${job} — ${e.error}`;
      case 'itemStart':
        return `${e.kind === 'classify' ? t('tasks.evtClassifying') : t('tasks.evtTranslating')}${title}…`;
      case 'itemDone':
        if (e.kind === 'classify') {
          return e.detail
            ? `${t('tasks.evtClassified')}${title} → ${e.detail}`
            : `${t('tasks.evtClassified')}${title}`;
        }
        return `${t('tasks.evtTranslated')}${title} ${e.detail} ${t('tasks.evtDone')}`;
      case 'itemSkip':
        return e.kind === 'classify'
          ? `△ ${title} ${t('tasks.evtUnmatched')}`
          : `△ ${title} ${t('tasks.evtSkipTranslate')}`;
      case 'itemError':
        return `✗ ${title} ${e.error}`;
      case 'summary':
        return `${t('tasks.evtSummary')} ${e.count ?? 0} ${t('tasks.unitArticles')}`;
      default:
        return `${e.type} ${title}`.trim();
    }
  }

  colorOf(e: TaskEvent): string {
    switch (e.type) {
      case 'itemDone':
      case 'jobDone':
      case 'summary':
        return 'var(--success-color)';
      case 'itemError':
      case 'jobError':
        return 'var(--error-color)';
      case 'itemSkip':
      case 'jobCancelled':
        return 'var(--warning-color)';
      default:
        return 'var(--text-secondary)';
    }
  }
}
