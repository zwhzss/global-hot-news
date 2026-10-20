import { Component, OnInit, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DataSourceService, Source, TestResult } from '../../services/datasource.service';

interface EditForm {
  name: string;
  type: string;
  url: string;
  /** For web_rss: the RSSHub route pattern (e.g. "toutiao/topic/12345"). Empty = auto-derive. */
  cronExpression: string;
  interval: number;
  publisher: string;
}

function emptyForm(): EditForm {
  return { name: '', type: 'rss', url: '', cronExpression: '', interval: 300, publisher: '' };
}

@Component({
  selector: 'app-datasource',
  standalone: false,
  template: `
    <div class="page-header flex flex-between">
      <h2>Data Sources</h2>
      <button class="btn btn-primary" (click)="openAddModal()" [disabled]="!pageReady()">+ Add Source</button>
    </div>
    <div class="page-content">
      @if (!pageReady()) {
        <div class="loading">Loading...</div>
      } @else {
        <table class="data-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Type</th>
              <th>URL</th>
              <th>Status</th>
              <th>Interval</th>
              <th>Last Fetch</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            @for (src of sources(); track src.id) {
              <tr>
                <td>{{ src.name }}</td>
                <td><span class="status-badge">{{ src.type }}</span></td>
                <td><a href="{{ src.url }}" target="_blank">{{ truncate(src.url, 50) }}</a></td>
                <td>
                  <span class="status-badge status-{{ src.status }}">{{ src.status }}</span>
                  @if (src.lastError) { <span class="text-sm text-secondary">{{ src.lastError }}</span> }
                </td>
                <td>{{ src.requestInterval }}s</td>
                <td>{{ src.lastFetchedAt ? formatDate(src.lastFetchedAt) : 'Never' }}</td>
                <td class="gap-8 flex">
                  <button class="btn btn-sm" (click)="toggleSource(src)" [disabled]="buttonLoading() === 'toggle-'+src.id">
                    {{ buttonLoading() === 'toggle-'+src.id ? '...' : (src.isActive ? 'Disable' : 'Enable') }}
                  </button>
                  <button class="btn btn-sm" (click)="testSource(src)" [disabled]="buttonLoading() === 'test-'+src.id">
                    {{ buttonLoading() === 'test-'+src.id ? 'Testing...' : 'Test' }}
                  </button>
                  <button class="btn btn-sm" (click)="fetchNow(src)" [disabled]="buttonLoading() === 'fetch-'+src.id">
                    {{ buttonLoading() === 'fetch-'+src.id ? 'Fetching...' : 'Fetch' }}
                  </button>
                  <button class="btn btn-sm btn-danger" (click)="deleteSource(src)" [disabled]="buttonLoading() === 'delete-'+src.id">
                    {{ buttonLoading() === 'delete-'+src.id ? '...' : 'Delete' }}
                  </button>
                </td>
              </tr>
            } @empty {
              <tr><td colspan="7" class="text-secondary text-center">No data sources configured. Add one to get started.</td></tr>
            }
          </tbody>
        </table>
      }

      <!-- Add/Edit Modal -->
      @if (showAddModal()) {
        <div class="modal-overlay" (click)="closeAddModal()">
          <div class="modal" (click)="$event.stopPropagation()">
            <h3>{{ editingSource() ? 'Edit Source' : 'Add Source' }}</h3>
            <div class="form-group">
              <label>Name</label>
              <input [ngModel]="editForm().name" (ngModelChange)="patchForm({ name: $event })" placeholder="Source name">
            </div>
            <div class="form-group">
              <label>Type</label>
              <select [ngModel]="editForm().type" (ngModelChange)="patchForm({ type: $event })">
                <option value="rss">RSS/Atom</option>
                <option value="web_rss">Web RSS (via RSSHub)</option>
                <option value="html">HTML List</option>
                <option value="api">JSON API</option>
              </select>
            </div>
            <div class="form-group">
              <label>URL</label>
              <input [ngModel]="editForm().url" (ngModelChange)="patchForm({ url: $event })" placeholder="https://...">
              @if (editForm().type === 'web_rss') {
                <div class="text-sm text-secondary" style="margin-top:4px;">
                  Original webpage URL. RSSHub will convert it to an RSS feed.
                </div>
              }
            </div>
            <div class="form-group">
              <label>Publisher (original source name)</label>
              <input [ngModel]="editForm().publisher" (ngModelChange)="patchForm({ publisher: $event })" placeholder="e.g. 人民网, 央视新闻 — shown under each article">
              @if (editForm().type === 'web_rss') {
                <div class="text-sm text-secondary" style="margin-top:4px;">
                  Leave empty to auto-detect from RSS &lt;source&gt; tag.
                </div>
              }
            </div>
            <div class="form-group">
              <label>Request Interval (seconds)</label>
              <input type="number" [ngModel]="editForm().interval" (ngModelChange)="patchForm({ interval: +$event })" min="10">
            </div>
            <div class="modal-actions">
              <button class="btn" (click)="closeAddModal()">Cancel</button>
              <button class="btn btn-primary" (click)="saveSource()" [disabled]="saving()">
                {{ saving() ? 'Saving...' : 'Save' }}
              </button>
            </div>
          </div>
        </div>
      }

      <!-- Test Result Modal -->
      @if (testResult(); as result) {
        <div class="modal-overlay" (click)="closeTestResult()">
          <div class="modal" (click)="$event.stopPropagation()">
            <h3>Test Result</h3>
            <p><strong>Available:</strong> {{ result.available ? 'Yes' : 'No' }}</p>
            <p><strong>Message:</strong> {{ result.message }}</p>
            @if (result.samples && result.samples.length > 0) {
              <div class="mt-16">
                <h4>Samples ({{ result.samples.length }}):</h4>
                @for (s of result.samples; track $index) {
                  <div class="text-sm mb-16">- {{ s.title }}</div>
                }
              </div>
            }
            <div class="modal-actions">
              <button class="btn" (click)="closeTestResult()">Close</button>
            </div>
          </div>
        </div>
      }
    </div>
  `,
})
export class DataSourcePage implements OnInit {
  // NOTE: this application runs with Angular's zoneless change detection
  // (zone.js is not a dependency and is not in the build's polyfills).
  // State that is assigned *after* an `await` therefore must be signal-based,
  // otherwise no change detection pass is scheduled and the view stays stale.
  readonly sources = signal<Source[]>([]);
  readonly pageReady = signal(false);
  readonly showAddModal = signal(false);
  readonly editingSource = signal<Source | null>(null);
  readonly testResult = signal<TestResult | null>(null);
  readonly editForm = signal<EditForm>(emptyForm());
  readonly buttonLoading = signal('');
  readonly saving = signal(false);

  constructor(private dsService: DataSourceService) {}

  async ngOnInit() {
    try {
      await this.loadSources();
    } catch (error) {
      console.error('Failed to load data sources:', error);
      this.sources.set([]);
    } finally {
      this.pageReady.set(true);
    }
  }

  async loadSources() {
    try {
      this.sources.set(await this.dsService.list());
    } catch (e) {
      console.error('Failed to load sources:', e);
      this.sources.set([]);
    } finally {
      this.pageReady.set(true);
    }
  }

  patchForm(patch: Partial<EditForm>) {
    this.editForm.update((form) => ({ ...form, ...patch }));
  }

  closeAddModal() {
    this.showAddModal.set(false);
  }

  closeTestResult() {
    this.testResult.set(null);
  }

  openAddModal() {
    if (!this.pageReady()) return;
    this.editingSource.set(null);
    this.editForm.set(emptyForm());
    this.showAddModal.set(true);
  }

  openEditModal(src: Source) {
    this.editingSource.set(src);
    this.editForm.set({
      name: src.name,
      type: src.type,
      url: src.url,
      cronExpression: src.cronExpression,
      interval: src.requestInterval,
      publisher: src.publisher,
    });
    this.showAddModal.set(true);
  }

  async saveSource() {
    const form = this.editForm();
    if (!form.name || !form.url) {
      alert('Name and URL are required');
      return;
    }
    this.saving.set(true);
    try {
      const editing = this.editingSource();
      if (editing) {
        await this.dsService.update(editing.id, form.name, form.type, form.url, form.interval, form.publisher);
      } else {
        await this.dsService.create(form.name, form.type, form.url, form.cronExpression, form.interval, form.publisher);
      }
      this.showAddModal.set(false);
      await this.loadSources();
    } catch (e) {
      alert('Failed to save source: ' + e);
    } finally {
      this.saving.set(false);
    }
  }

  async deleteSource(src: Source) {
    if (!confirm(`Delete source "${src.name}"?`)) return;
    this.buttonLoading.set('delete-' + src.id);
    try {
      await this.dsService.delete(src.id);
    } catch (e) {
      alert('Failed to delete: ' + e);
    } finally {
      this.buttonLoading.set('');
      await this.loadSources();
    }
  }

  async toggleSource(src: Source) {
    this.buttonLoading.set('toggle-' + src.id);
    try {
      await this.dsService.toggleActive(src.id, !src.isActive);
      await this.loadSources();
    } catch (e) {
      alert('Failed to toggle: ' + e);
    } finally {
      this.buttonLoading.set('');
    }
  }

  async testSource(src: Source) {
    this.buttonLoading.set('test-' + src.id);
    this.testResult.set(null);
    try {
      this.testResult.set(await this.dsService.test(src.id));
    } catch (e) {
      this.testResult.set({ available: false, message: 'Test failed: ' + e });
    } finally {
      this.buttonLoading.set('');
    }
  }

  async fetchNow(src: Source) {
    this.buttonLoading.set('fetch-' + src.id);
    try {
      const count = await this.dsService.fetchNow(src.id);
      alert(`Fetched ${count} new articles from ${src.name}`);
      await this.loadSources();
    } catch (e) {
      alert('Fetch failed: ' + e);
    } finally {
      this.buttonLoading.set('');
    }
  }

  formatDate(dateStr: string): string {
    if (!dateStr) return '';
    return new Date(dateStr).toLocaleString();
  }

  truncate(str: string, len: number): string {
    return str.length > len ? str.substring(0, len) + '...' : str;
  }
}
