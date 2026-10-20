import { Injectable } from '@angular/core';
import { TaskLogClear, TaskLogRecent } from '../../../wailsjs/go/bindings/Bindings';

/** One line of the task monitor feed (mirrors llmqueue.Event on the Go side). */
export interface TaskEvent {
  id: number;
  time: string;
  jobId: number;
  kind: string; // 'translate' | 'classify'
  jobName: string;
  type: string; // jobQueued|jobStart|jobDone|jobError|jobCancelled|itemStart|itemDone|itemSkip|itemError|summary
  articleId?: number;
  title?: string;
  detail?: string;
  count?: number;
  error?: string;
}

/** Read side of the in-memory event ring behind the /tasks page. */
@Injectable({ providedIn: 'root' })
export class TaskService {
  /** Newest events in ascending id order (the page renders them reversed). */
  async recent(limit = 200): Promise<TaskEvent[]> {
    const raw: unknown = await TaskLogRecent(limit);
    const list = Array.isArray(raw) ? (raw as Record<string, unknown>[]) : [];
    return list.map((e) => ({
      id: Number(e['id'] ?? 0),
      time: String(e['time'] ?? ''),
      jobId: Number(e['jobId'] ?? 0),
      kind: String(e['kind'] ?? ''),
      jobName: String(e['jobName'] ?? ''),
      type: String(e['type'] ?? ''),
      articleId: e['articleId'] === undefined ? undefined : Number(e['articleId']),
      title: String(e['title'] ?? ''),
      detail: String(e['detail'] ?? ''),
      count: e['count'] === undefined ? undefined : Number(e['count']),
      error: String(e['error'] ?? ''),
    }));
  }

  /** Drops the whole feed ("clear" on the monitor page). */
  async clear(): Promise<void> {
    await TaskLogClear();
  }
}
