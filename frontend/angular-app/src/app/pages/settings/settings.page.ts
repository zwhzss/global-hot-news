import { Component, OnInit, OnDestroy, computed, signal } from '@angular/core';
import { I18nService } from '../../services/i18n.service';
import { SettingService } from '../../services/setting.service';
import { PlaybackService } from '../../services/playback.service';
import { AuditCounts, LocalModel, QueueJob, QueueStatus, TranslationService, TranslationStats, TranslationStatus, AuditResult } from '../../services/translation.service';

interface SettingsMap {
  max_concurrency: string;
  default_interval_seconds: string;
  article_ttl_days: string;
  log_level: string;
  compliance_filter_enabled: string;
  robots_enforcement: string;
  auto_collect: string;
  translate_enabled: string;
  translate_provider: string;
  translate_engine: string;
  mtranserver_url: string;
  mtranserver_token: string;
  libretranslate_url: string;
  libretranslate_api_key: string;
  cls_base_url: string;
  cls_model: string;
  cls_api_key: string;
  llm_base_url: string;
  llm_model: string;
  llm_api_key: string;
  translate_scope: string;
  translate_batch_size: string;
  llm_max_concurrency: string;
  translate_timeout_seconds: string;
  translate_daily_limit: string;
  tts_enabled: string;
  tts_engine: string;
  tts_voice: string;
  tts_max_per_round: string;
  tts_concurrency: string;
  tts_auto_play: string;
  tts_python: string;
  tts_api_url: string;
  tts_api_key: string;
  tts_api_model: string;
  tts_api_voice: string;
}

function emptySettings(): SettingsMap {
  return {
    max_concurrency: '3',
    default_interval_seconds: '300',
    article_ttl_days: '90',
    log_level: 'info',
    compliance_filter_enabled: 'true',
    robots_enforcement: 'true',
    auto_collect: 'true',
    // Free first: a local model on 127.0.0.1 costs nothing and keeps every
    // headline on this machine.
    translate_enabled: 'false',
    translate_provider: 'local',
    translate_engine: 'llm',
    mtranserver_url: 'http://localhost:8989',
    mtranserver_token: '',
    libretranslate_url: 'http://localhost:5000',
    libretranslate_api_key: '',
    cls_base_url: 'http://127.0.0.1:11434/v1',
    cls_model: 'qwen2.5:7b',
    cls_api_key: '',
    llm_base_url: 'http://127.0.0.1:11434/v1',
    llm_model: 'qwen2.5:7b',
    llm_api_key: '',
    translate_scope: 'title_summary',
    translate_batch_size: '10',
    llm_max_concurrency: '1',
    translate_timeout_seconds: '120',
    translate_daily_limit: '300',
    tts_enabled: 'true',
    tts_engine: 'edge',
    tts_voice: 'zh-CN-XiaoxiaoNeural',
    tts_max_per_round: '50',
    tts_concurrency: '3',
    tts_auto_play: 'true',
    tts_python: '',
    tts_api_url: '',
    tts_api_key: '',
    tts_api_model: '',
    tts_api_voice: '',
  };
}

@Component({
  selector: 'app-settings',
  standalone: false,
  template: `
    <div class="page-header">
      <h2>{{ i18n.t('nav.settings') }}</h2>
    </div>
    <div class="page-content">
      <div class="modal" style="max-width: 760px;">
        <div class="flex flex-between" style="align-items:center; margin-bottom: 16px;">
          <h3 style="margin:0;">{{ i18n.t('settings.title') }}</h3>
          @if (loading()) {
            <span class="text-sm text-secondary">{{ i18n.t('common.loading') }}</span>
          }
        </div>

        <div class="form-group">
          <label>{{ i18n.t('settings.maxConcurrency') }}</label>
          <input type="number" [ngModel]="settings().max_concurrency" (ngModelChange)="patch({ max_concurrency: $event })" min="1" max="10">
        </div>
        <div class="form-group">
          <label>{{ i18n.t('settings.defaultInterval') }}</label>
          <input type="number" [ngModel]="settings().default_interval_seconds" (ngModelChange)="patch({ default_interval_seconds: $event })" min="5" max="3600">
        </div>
        <div class="form-group">
          <label>{{ i18n.t('settings.articleTtl') }}</label>
          <input type="number" [ngModel]="settings().article_ttl_days" (ngModelChange)="patch({ article_ttl_days: $event })" min="1" max="365">
        </div>
        <div class="form-group">
          <label>{{ i18n.t('settings.logLevel') }}</label>
          <select [ngModel]="settings().log_level" (ngModelChange)="patch({ log_level: $event })">
            <option value="debug">Debug</option>
            <option value="info">Info</option>
            <option value="warn">Warn</option>
            <option value="error">Error</option>
          </select>
        </div>

        <hr style="margin: 16px 0;">

        <div class="form-group flex flex-between" style="align-items:center;">
          <label style="margin:0;">{{ i18n.t('settings.complianceFilter') }}</label>
          <label class="toggle">
            <input type="checkbox" [checked]="settings().compliance_filter_enabled === 'true'" (change)="toggle('compliance_filter_enabled', $event)">
            <span class="slider"></span>
          </label>
        </div>
        <div class="form-group flex flex-between" style="align-items:center;">
          <label style="margin:0;">{{ i18n.t('settings.robots') }}</label>
          <label class="toggle">
            <input type="checkbox" [checked]="settings().robots_enforcement === 'true'" (change)="toggle('robots_enforcement', $event)">
            <span class="slider"></span>
          </label>
        </div>
        <div class="form-group flex flex-between" style="align-items:center;">
          <label style="margin:0;">{{ i18n.t('settings.autoCollect') }}</label>
          <label class="toggle">
            <input type="checkbox" [checked]="settings().auto_collect === 'true'" (change)="toggle('auto_collect', $event)">
            <span class="slider"></span>
          </label>
        </div>

        <div class="modal-actions">
          <button class="btn" (click)="loadSettings()">{{ i18n.t('common.reset') }}</button>
          <button class="btn btn-primary" (click)="saveSettings()">{{ i18n.t('common.save') }}</button>
        </div>
        @if (generalMessage()) {
          <div class="alert alert-info mt-16">{{ generalMessage() }}</div>
        }
        @if (generalError()) {
          <div class="alert alert-error mt-16">{{ generalError() }}</div>
        }

        <!-- ============ Classification model ============ -->
        <hr style="margin: 24px 0;">
        <h3>{{ i18n.t('cls.section') }}</h3>
        <p class="text-sm text-secondary" style="margin: 8px 0 16px;">{{ i18n.t('cls.hint') }}</p>

        <div class="form-group">
          <label>{{ i18n.t('cls.baseUrl') }}</label>
          <input type="text" [ngModel]="settings().cls_base_url" (ngModelChange)="patch({ cls_base_url: $event })">
        </div>
        <div class="form-group">
          <label>{{ i18n.t('cls.model') }}</label>
          <input type="text" [ngModel]="settings().cls_model" (ngModelChange)="patch({ cls_model: $event })">
        </div>
        <div class="form-group">
          <label>{{ i18n.t('cls.apiKey') }}</label>
          <input type="password" [ngModel]="settings().cls_api_key" (ngModelChange)="patch({ cls_api_key: $event })">
        </div>

        <div class="modal-actions">
          <button class="btn btn-sm" [disabled]="clsTesting()" (click)="testClassifier()">
            {{ clsTesting() ? i18n.t('cls.testing') : i18n.t('cls.test') }}
          </button>
          <button class="btn btn-primary" (click)="saveClassifier()">{{ i18n.t('common.save') }}</button>
        </div>
        @if (clsMessage()) {
          <div class="alert alert-info mt-16">{{ clsMessage() }}</div>
        }
        @if (clsError()) {
          <div class="alert alert-error mt-16">{{ clsError() }}</div>
        }

        <!-- ============ Bilingual translation ============ -->
        <hr style="margin: 24px 0;">
        <h3>{{ i18n.t('trans.section') }}</h3>
        <p class="text-sm text-secondary" style="margin: 8px 0 16px;">{{ i18n.t('trans.hint') }}</p>

        <div class="form-group flex flex-between" style="align-items:center;">
          <label style="margin:0;">{{ i18n.t('trans.enabled') }}</label>
          <label class="toggle">
            <input type="checkbox" [checked]="settings().translate_enabled === 'true'" (change)="toggle('translate_enabled', $event)">
            <span class="slider"></span>
          </label>
        </div>

        <div class="form-group">
          <label>{{ i18n.t('trans.engine') }}</label>
          <div class="chips">
            <button class="chip" [class.active]="settings().translate_engine === 'llm'" (click)="setEngine('llm')">
              {{ i18n.t('trans.engineLLM') }}
            </button>
            <button class="chip" [class.active]="settings().translate_engine === 'mtranserver'" (click)="setEngine('mtranserver')">
              {{ i18n.t('trans.engineMTran') }}
            </button>
            <button class="chip" [class.active]="settings().translate_engine === 'libretranslate'" (click)="setEngine('libretranslate')">
              {{ i18n.t('trans.engineLibre') }}
            </button>
          </div>
        </div>

        @if (settings().translate_engine === 'llm') {
        <div class="form-group">
          <label>{{ i18n.t('trans.section') }}</label>
          <div class="chips">
            <button class="chip" [class.active]="settings().translate_provider === 'local'" (click)="setProvider('local')">
              {{ i18n.t('trans.modeLocal') }}
            </button>
            <button class="chip" [class.active]="settings().translate_provider === 'cloud'" (click)="setProvider('cloud')">
              {{ i18n.t('trans.modeCloud') }}
            </button>
            <button class="chip" [class.active]="settings().translate_provider === 'off'" (click)="setProvider('off')">
              {{ i18n.t('trans.modeOff') }}
            </button>
          </div>
        </div>

        <div class="form-group">
          <button class="btn btn-sm" [disabled]="detecting()" (click)="detectLocal()">
            {{ detecting() ? i18n.t('trans.detecting') : i18n.t('trans.detect') }}
          </button>
          @if (detected().length > 0) {
            <div class="mt-16">
              <div class="text-sm text-secondary">{{ i18n.t('trans.detected') }}</div>
              @for (m of detected(); track m.baseUrl) {
                <div class="detected-model">
                  <span><strong>{{ m.name }}</strong> · {{ m.baseUrl }}</span>
                  <span class="text-sm text-secondary">{{ m.models.join(', ') }}</span>
                  <button class="btn btn-sm btn-primary" (click)="applyLocal(m)">{{ i18n.t('common.save') }}</button>
                </div>
              }
            </div>
          } @else if (detectRan()) {
            <p class="text-sm text-secondary" style="margin-top: 8px;">{{ i18n.t('trans.noneDetected') }}</p>
          }
        </div>

        <div class="form-group">
          <label>{{ i18n.t('trans.baseUrl') }}</label>
          <input type="text" [ngModel]="settings().llm_base_url" (ngModelChange)="patch({ llm_base_url: $event })">
        </div>
        <div class="form-group">
          <label>{{ i18n.t('trans.model') }}</label>
          <input type="text" [ngModel]="settings().llm_model" (ngModelChange)="patch({ llm_model: $event })" list="model-options">
          <datalist id="model-options">
            @for (m of availableModels(); track m) {
              <option [value]="m"></option>
            }
          </datalist>
        </div>
        <div class="form-group">
          <label>{{ i18n.t('trans.apiKey') }}</label>
          <input type="password" [ngModel]="settings().llm_api_key" (ngModelChange)="patch({ llm_api_key: $event })">
        </div>
        } @else if (settings().translate_engine === 'mtranserver') {
        <div class="form-group">
          <label>{{ i18n.t('trans.mtranUrl') }}</label>
          <input type="text" [ngModel]="settings().mtranserver_url" (ngModelChange)="patch({ mtranserver_url: $event })">
          <p class="text-sm text-secondary" style="margin: 4px 0 0;">{{ i18n.t('trans.mtranHint') }}</p>
        </div>
        <div class="form-group">
          <label>{{ i18n.t('trans.mtranToken') }}</label>
          <input type="password" [ngModel]="settings().mtranserver_token" (ngModelChange)="patch({ mtranserver_token: $event })">
        </div>
        } @else {
        <div class="form-group">
          <label>{{ i18n.t('trans.libreUrl') }}</label>
          <input type="text" [ngModel]="settings().libretranslate_url" (ngModelChange)="patch({ libretranslate_url: $event })">
          <p class="text-sm text-secondary" style="margin: 4px 0 0;">{{ i18n.t('trans.libreHint') }}</p>
        </div>
        <div class="form-group">
          <label>{{ i18n.t('trans.libreKey') }}</label>
          <input type="password" [ngModel]="settings().libretranslate_api_key" (ngModelChange)="patch({ libretranslate_api_key: $event })">
        </div>
        }

        <div class="form-group">
          <label>{{ i18n.t('trans.scope') }}</label>
          <select [ngModel]="settings().translate_scope" (ngModelChange)="patch({ translate_scope: $event })">
            <option value="title">{{ i18n.t('trans.scopeTitle') }}</option>
            <option value="title_summary">{{ i18n.t('trans.scopeTitleSummary') }}</option>
            <option value="all">{{ i18n.t('trans.scopeAll') }}</option>
          </select>
        </div>
        <div class="form-group">
          <label>{{ i18n.t('trans.batchSize') }}</label>
          <input type="number" [ngModel]="settings().translate_batch_size" (ngModelChange)="patch({ translate_batch_size: $event })" min="1" max="50">
        </div>
        <div class="form-group">
          <label>{{ i18n.t('trans.maxConcurrency') }}</label>
          <input type="number" [ngModel]="settings().llm_max_concurrency" (ngModelChange)="patch({ llm_max_concurrency: $event })" min="1" max="8">
          <p class="text-sm text-secondary" style="margin: 4px 0 0;">{{ i18n.t('trans.maxConcurrencyHint') }}</p>
        </div>
        <div class="form-group">
          <label>{{ i18n.t('trans.timeout') }}</label>
          <input type="number" [ngModel]="settings().translate_timeout_seconds" (ngModelChange)="patch({ translate_timeout_seconds: $event })" min="10" max="600">
        </div>
        <div class="form-group">
          <label>{{ i18n.t('trans.dailyLimit') }}</label>
          <input type="number" [ngModel]="settings().translate_daily_limit" (ngModelChange)="patch({ translate_daily_limit: $event })" min="0" max="100000">
        </div>

        <div class="modal-actions">
          <button class="btn btn-sm" [disabled]="testing()" (click)="testConnection()">
            {{ testing() ? i18n.t('trans.testing') : i18n.t('trans.test') }}
          </button>
          <button class="btn btn-sm" [disabled]="translating()" (click)="translateBusy() ? cancelTranslate() : runNow()">
            @if (translateBusy()) {
              {{ i18n.t('trans.cancelTranslate') }}
            } @else if (translating()) {
              {{ i18n.t('trans.running') }}
            } @else {
              {{ i18n.t('trans.runNow') }}
            }
          </button>
          <button class="btn btn-sm" (click)="resetFailed()">{{ i18n.t('trans.resetFailed') }}</button>
          <button class="btn btn-primary" (click)="saveTranslation()">{{ i18n.t('common.save') }}</button>
        </div>

        @if (actionMessage()) {
          <div class="alert alert-info mt-16">{{ actionMessage() }}</div>
        }
        @if (actionError()) {
          <div class="alert alert-error mt-16">{{ actionError() }}</div>
        }

        <!-- status -->
        <hr style="margin: 24px 0;">
        <h3>{{ i18n.t('trans.status') }}</h3>
        @if (status(); as st) {
          <div class="stat-grid mt-16">
            <div class="stat-card">
              <div class="stat-label">{{ i18n.t('trans.configured') }}</div>
              <div class="stat-value">{{ st.configured ? i18n.t('common.yes') : i18n.t('common.no') }}</div>
            </div>
            <div class="stat-card">
              <div class="stat-label">{{ i18n.t('trans.reachable') }}</div>
              <div class="stat-value" [class.text-error]="!st.reachable">
                {{ st.reachable ? i18n.t('common.yes') : i18n.t('common.no') }}
              </div>
            </div>
            <div class="stat-card">
              <div class="stat-label">{{ i18n.t('trans.model') }}</div>
              <div class="stat-value">{{ st.model || '-' }}</div>
            </div>
            <div class="stat-card">
              <div class="stat-label">{{ i18n.t('trans.translatedToday') }}</div>
              <div class="stat-value">{{ st.translatedToday }}</div>
            </div>
          </div>
          <p class="text-sm text-secondary mt-16">
            {{ i18n.t('trans.baseUrl') }}: {{ st.baseUrl }} · {{ st.engine || 'llm' }}@if (st.engine === 'llm' || !st.engine) {<span> · {{ st.local ? i18n.t('trans.modeLocal') : i18n.t('trans.modeCloud') }}</span>}
          </p>
          @if (st.lastRun) {
            <p class="text-sm text-secondary">{{ i18n.t('trans.lastRun') }}: {{ st.lastRun }}</p>
          }
          @if (st.lastError) {
            <p class="text-sm text-error">{{ st.lastError }}</p>
          }
        }

        @if (stats(); as s) {
          <div class="stat-grid mt-16">
            <div class="stat-card"><div class="stat-label">{{ i18n.t('trans.statTotal') }}</div><div class="stat-value">{{ s.total }}</div></div>
            <div class="stat-card"><div class="stat-label">{{ i18n.t('trans.statDone') }}</div><div class="stat-value">{{ s.done }}</div></div>
            <div class="stat-card"><div class="stat-label">{{ i18n.t('trans.statPending') }}</div><div class="stat-value">{{ s.processable }}{{ s.pending !== s.processable ? ' / ' + s.pending : '' }}</div></div>
            <div class="stat-card"><div class="stat-label">{{ i18n.t('trans.statFailed') }}</div><div class="stat-value">{{ s.failed }}</div></div>
            <div class="stat-card"><div class="stat-label">{{ i18n.t('trans.statSkipped') }}</div><div class="stat-value">{{ s.skipped }}</div></div>
            <div class="stat-card"><div class="stat-label">{{ i18n.t('trans.statCache') }}</div><div class="stat-value">{{ s.cacheHit }}</div></div>
          </div>
        }

        <hr style="margin: 24px 0;">
        <h3>{{ i18n.t('trans.auditTitle') }}</h3>
        <p class="text-sm text-secondary" style="margin: 8px 0 16px;">{{ i18n.t('trans.auditHint') }}</p>
        @if (queue(); as q) {
          @if (q.queued > 0 || q.current) {
            <p class="text-sm text-secondary" style="margin: 0 0 12px;">
              {{ i18n.t('trans.queueLabel') }}: {{ q.queued + (q.current ? 1 : 0) }}
              @if (q.current; as cur) {
                · {{ i18n.t('trans.currentJob') }}: {{ jobLabel(cur) }}
              }
              · <a routerLink="/tasks">{{ i18n.t('tasks.watch') }}</a>
            </p>
          }
        }
        <div class="modal-actions">
          <button class="btn btn-sm" [disabled]="auditing()" (click)="runAudit()">
            {{ auditing() ? i18n.t('trans.auditing') : i18n.t('trans.runAudit') }}
            @if (counts(); as c) { <span class="audit-count">({{ c.total }})</span> }
          </button>
          @if (unclassifiedJobRunning()) {
            <button class="btn btn-sm" (click)="cancelClassify()">
              {{ i18n.t('trans.cancelClassify') }}
            </button>
          } @else {
            <button class="btn btn-sm" [disabled]="classifyingUnclassified() || classifyJobRunning()" (click)="classifyUnclassified()">
              @if (classifyingUnclassified()) {
                {{ i18n.t('trans.classifying') }}
              } @else {
                {{ i18n.t('trans.classifyUnclassified') }}
                @if (counts(); as c) { <span class="audit-count">({{ c.unclassified }})</span> }
              }
            </button>
          }
          @if (reclassifyJobRunning()) {
            <button class="btn btn-sm" (click)="cancelClassify()">
              {{ i18n.t('trans.cancelClassify') }}
            </button>
          } @else {
            <button class="btn btn-sm" [disabled]="reclassifyingSuspects() || classifyJobRunning()" (click)="reclassifySuspects()">
              @if (reclassifyingSuspects()) {
                {{ i18n.t('trans.reclassifying') }}
              } @else {
                {{ i18n.t('trans.reclassifySuspects') }}
                <span class="audit-count">({{ suspectCount() }})</span>
              }
            </button>
          }
          @if (queueBusy()) {
            <button class="btn btn-sm btn-danger" (click)="cancelAll()">
              {{ i18n.t('trans.cancelAll') }}
            </button>
          }
        </div>
        @if (auditMessage()) {
          <div class="alert alert-info mt-16">{{ auditMessage() }}</div>
        }
        @if (auditError()) {
          <div class="alert alert-error mt-16">{{ auditError() }}</div>
        }

        @if (audit(); as a) {
          <div class="mt-16">
            <h4>{{ i18n.t('trans.auditCategories') }}</h4>
            <table class="audit-table">
              <thead>
                <tr>
                  <th>{{ i18n.t('trans.auditCategory') }}</th>
                  <th>{{ i18n.t('trans.auditTotal') }}</th>
                  <th>{{ i18n.t('trans.auditWeak') }}</th>
                </tr>
              </thead>
              <tbody>
                @for (c of a.categories; track c.categoryId) {
                  <tr>
                    <td>{{ c.category }}</td>
                    <td>{{ c.total }}</td>
                    <td [class.text-error]="c.weak > 0">{{ c.weak }}</td>
                  </tr>
                }
              </tbody>
            </table>

            @if (a.suspects.length > 0) {
              <h4 class="mt-16">{{ i18n.t('trans.auditSuspects') }} ({{ a.suspects.length }})</h4>
              <table class="audit-table">
                <thead>
                  <tr>
                    <th>{{ i18n.t('trans.auditTitle') }}</th>
                    <th>{{ i18n.t('trans.auditCategory') }}</th>
                    <th>{{ i18n.t('trans.auditKeyword') }}</th>
                  </tr>
                </thead>
                <tbody>
                  @for (s of a.suspects; track s.articleId) {
                    <tr>
                      <td class="audit-title">{{ s.title }}</td>
                      <td>{{ s.category }}</td>
                      <td class="text-error">{{ s.keyword }}</td>
                    </tr>
                  }
                </tbody>
              </table>
            } @else {
              <p class="text-sm text-secondary mt-16">{{ i18n.t('trans.auditClean') }}</p>
            }
          </div>
        }

        <!-- ============ TTS narration ============ -->
        <hr style="margin: 24px 0;">
        <h3>{{ i18n.t('tts.section') }}</h3>
        <p class="text-sm text-secondary" style="margin: 8px 0 16px;">{{ i18n.t('tts.hint') }}</p>

        <div class="form-group flex flex-between" style="align-items:center;">
          <label style="margin:0;">{{ i18n.t('tts.enabled') }}</label>
          <label class="toggle">
            <input type="checkbox" [checked]="settings().tts_enabled === 'true'" (change)="toggle('tts_enabled', $event)">
            <span class="slider"></span>
          </label>
        </div>

        <div class="form-group">
          <label>{{ i18n.t('tts.engine') }}</label>
          <div class="chips">
            <button class="chip" [class.active]="settings().tts_engine !== 'api'" (click)="patch({ tts_engine: 'edge' })">
              {{ i18n.t('tts.engineEdge') }}
            </button>
            <button class="chip" [class.active]="settings().tts_engine === 'api'" (click)="patch({ tts_engine: 'api' })">
              {{ i18n.t('tts.engineAPI') }}
            </button>
          </div>
        </div>

        @if (settings().tts_engine === 'api') {
        <div class="form-group">
          <label>{{ i18n.t('tts.apiUrl') }}</label>
          <input type="text" [ngModel]="settings().tts_api_url" (ngModelChange)="patch({ tts_api_url: $event })" placeholder="https://api.openai.com/v1">
          <p class="text-sm text-secondary" style="margin: 4px 0 0;">{{ i18n.t('tts.apiUrlHint') }}</p>
        </div>
        <div class="form-group">
          <label>{{ i18n.t('tts.apiKey') }}</label>
          <input type="password" [ngModel]="settings().tts_api_key" (ngModelChange)="patch({ tts_api_key: $event })">
        </div>
        <div class="form-group">
          <label>{{ i18n.t('tts.apiModel') }}</label>
          <input type="text" [ngModel]="settings().tts_api_model" (ngModelChange)="patch({ tts_api_model: $event })" placeholder="tts-1">
        </div>
        <div class="form-group">
          <label>{{ i18n.t('tts.apiVoice') }}</label>
          <input type="text" [ngModel]="settings().tts_api_voice" (ngModelChange)="patch({ tts_api_voice: $event })" placeholder="alloy">
        </div>
        } @else {
        <div class="form-group">
          <label>{{ i18n.t('tts.voice') }}</label>
          <input type="text" [ngModel]="settings().tts_voice" (ngModelChange)="patch({ tts_voice: $event })" list="tts-voice-options">
          <datalist id="tts-voice-options">
            <option value="zh-CN-XiaoxiaoNeural"></option>
            <option value="zh-CN-XiaoyiNeural"></option>
            <option value="zh-CN-YunxiNeural"></option>
            <option value="zh-CN-YunyangNeural"></option>
            <option value="zh-CN-YunjianNeural"></option>
            <option value="zh-CN-YunxiaNeural"></option>
            <option value="zh-TW-HsiaoChenNeural"></option>
            <option value="en-US-JennyNeural"></option>
          </datalist>
        </div>
        <div class="form-group">
          <label>{{ i18n.t('tts.python') }}</label>
          <input type="text" [ngModel]="settings().tts_python" (ngModelChange)="patch({ tts_python: $event })" placeholder="python">
          <p class="text-sm text-secondary" style="margin: 4px 0 0;">{{ i18n.t('tts.pythonHint') }}</p>
        </div>
        }

        <div class="form-group">
          <label>{{ i18n.t('tts.maxPerRound') }}</label>
          <input type="number" [ngModel]="settings().tts_max_per_round" (ngModelChange)="patch({ tts_max_per_round: $event })" min="0" max="500">
        </div>
        <div class="form-group">
          <label>{{ i18n.t('tts.concurrency') }}</label>
          <input type="number" [ngModel]="settings().tts_concurrency" (ngModelChange)="patch({ tts_concurrency: $event })" min="1" max="8">
          <p class="text-sm text-secondary" style="margin: 4px 0 0;">{{ i18n.t('tts.concurrencyHint') }}</p>
        </div>
        <div class="form-group flex flex-between" style="align-items:center;">
          <label style="margin:0;">{{ i18n.t('tts.autoPlay') }}</label>
          <label class="toggle">
            <input type="checkbox" [checked]="settings().tts_auto_play === 'true'" (change)="toggle('tts_auto_play', $event)">
            <span class="slider"></span>
          </label>
        </div>

        <div class="modal-actions">
          <button class="btn btn-sm" [disabled]="ttsTesting()" (click)="testTts()">
            {{ ttsTesting() ? i18n.t('tts.testing') : i18n.t('tts.test') }}
          </button>
          <button class="btn btn-primary" (click)="saveTts()">{{ i18n.t('common.save') }}</button>
        </div>
        @if (ttsMessage()) {
          <div class="alert alert-info mt-16">{{ ttsMessage() }}</div>
        }
        @if (ttsError()) {
          <div class="alert alert-error mt-16">{{ ttsError() }}</div>
        }
        @if (ttsAudio(); as audioUrl) {
          <audio controls [src]="audioUrl" style="width:100%; margin-top:10px;"></audio>
        }

        <hr style="margin: 24px 0;">
        <h3>{{ i18n.t('settings.about') }}</h3>
        <p class="text-sm text-secondary">{{ i18n.t('settings.aboutText') }}</p>
      </div>
    </div>
  `,
})
export class SettingsPage implements OnInit, OnDestroy {
  // NOTE: zoneless change detection — state assigned after `await` must be signal-based.
  readonly settings = signal<SettingsMap>(emptySettings());
  readonly loading = signal(false);
  readonly status = signal<TranslationStatus | null>(null);
  readonly stats = signal<TranslationStats | null>(null);
  readonly detected = signal<LocalModel[]>([]);
  readonly availableModels = signal<string[]>([]);
  readonly detecting = signal(false);
  readonly detectRan = signal(false);
  readonly testing = signal(false);
  readonly translating = signal(false);
  readonly actionMessage = signal('');
  readonly actionError = signal('');
  // The classification section renders its own feedback right under its
  // buttons; sharing actionMessage would show the result down in the
  // translation section, where nobody is looking.
  readonly clsTesting = signal(false);
  readonly clsMessage = signal('');
  readonly clsError = signal('');
  // Same idea for the general and audit sections: each posts its result right
  // under its own buttons instead of hundreds of lines down the page.
  readonly generalMessage = signal('');
  readonly generalError = signal('');
  readonly auditMessage = signal('');
  readonly auditError = signal('');
  // The TTS section posts its result (and the test clip URL) right under its
  // own buttons, same as the classifier section above it.
  readonly ttsTesting = signal(false);
  readonly ttsMessage = signal('');
  readonly ttsError = signal('');
  readonly ttsAudio = signal('');
  readonly audit = signal<AuditResult | null>(null);
  readonly auditing = signal(false);
  readonly classifyingUnclassified = signal(false);
  readonly reclassifyingSuspects = signal(false);
  readonly counts = signal<AuditCounts | null>(null);
  readonly queue = signal<QueueStatus | null>(null);
  /** Anything queued or running on the shared LLM queue. */
  readonly queueBusy = computed(() => {
    const q = this.queue();
    return !!q && (q.queued > 0 || q.current !== null);
  });
  /** Name of the classification job currently executing ('' = none). */
  readonly classifyJobName = computed(() => {
    const j = this.queue()?.current;
    return j && j.kind === 'classify' ? j.name : '';
  });
  readonly classifyJobRunning = computed(() => this.classifyJobName() !== '');
  /** True while the "classify unclassified" job is the one executing. */
  readonly unclassifiedJobRunning = computed(() =>
    this.classifyJobName() === 'audit-unclassified' || this.classifyJobName() === 'fill-unclassified');
  readonly reclassifyJobRunning = computed(() => this.classifyJobName() === 'audit-reclassify');
  /** A translation job (batch or single article) is executing, or a run is active. */
  readonly translateBusy = computed(
    () => this.queue()?.current?.kind === 'translate' || !!this.status()?.isRunning);
  readonly suspectCount = computed(() => this.audit()?.suspects.length ?? 0);
  private prevClassifyJob = '';
  private poll: ReturnType<typeof setInterval> | null = null;
  private pollFast = true;

  constructor(
    public i18n: I18nService,
    private settingService: SettingService,
    private translationService: TranslationService,
    private playback: PlaybackService
  ) {}

  async ngOnInit() {
    await this.loadSettings();
    await this.refreshTranslation();
    this.startPolling();
    // Initial audit so the button counts (total/suspects) show without a click.
    await this.runAudit();
  }

  ngOnDestroy() {
    this.stopPolling();
  }

  private startPolling() {
    this.stopPolling();
    this.poll = setInterval(() => this.refreshTranslation(), this.pollFast ? 1000 : 3000);
  }

  private stopPolling() {
    if (this.poll) {
      clearInterval(this.poll);
      this.poll = null;
    }
  }

  async loadSettings() {
    this.loading.set(true);
    try {
      const raw = await this.settingService.getAll();
      const defaults = emptySettings();
      const merged = { ...defaults };
      for (const key of Object.keys(defaults) as (keyof SettingsMap)[]) {
        if (raw[key] !== undefined && raw[key] !== '') {
          merged[key] = raw[key];
        }
      }
      this.settings.set(merged);
    } catch (e) {
      console.error('Failed to load settings:', e);
    } finally {
      this.loading.set(false);
    }
  }

  patch(p: Partial<SettingsMap>) {
    // <input type="number"> emits a number; keep the map strictly string-valued
    // so saving never hands a non-string to the Go setting API.
    const norm: Partial<SettingsMap> = {};
    for (const key of Object.keys(p) as (keyof SettingsMap)[]) {
      norm[key] = String(p[key] ?? '');
    }
    this.settings.update((s) => ({ ...s, ...norm }));
  }

  toggle(key: keyof SettingsMap, event: Event) {
    const checked = (event.target as HTMLInputElement).checked;
    this.patch({ [key]: checked ? 'true' : 'false' } as Partial<SettingsMap>);
    // Apply immediately: a toggle has no text to lose, and the user expects the
    // switch to take effect without hunting for Save.
    this.settingService.set(key, checked ? 'true' : 'false');
  }

  /** Switches the translation engine; only affects which fields the form shows. */
  setEngine(engine: string) {
    this.patch({ translate_engine: engine });
  }

  setProvider(provider: string) {
    // Presets: switching mode fills in the matching defaults so the free path
    // works with one click, while never silently overwriting a cloud key.
    if (provider === 'local') {
      const current = this.settings();
      const keepLocal = current.llm_base_url.includes('127.0.0.1') || current.llm_base_url.includes('localhost');
      this.patch({
        translate_provider: 'local',
        translate_enabled: 'true',
        llm_base_url: keepLocal ? current.llm_base_url : 'http://127.0.0.1:11434/v1',
        llm_model: keepLocal ? current.llm_model : 'qwen2.5:7b',
      });
    } else {
      this.patch({ translate_provider: provider });
    }
  }

  async detectLocal() {
    this.detecting.set(true);
    this.detectRan.set(true);
    this.actionError.set('');
    try {
      this.detected.set(await this.translationService.detectLocalModels());
    } catch (e: any) {
      this.actionError.set(String(e?.message || e));
    } finally {
      this.detecting.set(false);
    }
  }

  /** Applies a detected local server: base URL, model, and the free preset. */
  async applyLocal(m: LocalModel) {
    const model = m.models[0] || '';
    this.patch({
      translate_provider: 'local',
      translate_enabled: 'true',
      llm_base_url: m.baseUrl,
      llm_model: model,
    });
    await this.saveTranslation();
    this.availableModels.set(m.models);
    this.actionMessage.set(`${m.name} · ${m.baseUrl} · ${model}`);
  }

  async saveTranslation() {
    const s = this.settings();
    const keys: (keyof SettingsMap)[] = [
      'translate_enabled', 'translate_provider', 'translate_engine',
      'mtranserver_url', 'mtranserver_token', 'libretranslate_url', 'libretranslate_api_key',
      'llm_base_url', 'llm_model', 'llm_api_key',
      'translate_scope', 'translate_batch_size', 'llm_max_concurrency',
      'translate_timeout_seconds', 'translate_daily_limit',
    ];
    for (const k of keys) {
      await this.settingService.set(k, s[k]);
    }
    this.actionMessage.set(this.i18n.t('settings.saved'));
    await this.refreshTranslation();
  }

  async testConnection() {
    this.testing.set(true);
    this.actionMessage.set('');
    this.actionError.set('');
    try {
      // Save first: the backend reads its settings from the database, so an
      // unsaved key would make the test fail for the wrong reason.
      await this.saveTranslation();
      this.actionMessage.set(await this.translationService.test());
    } catch (e: any) {
      this.actionError.set(String(e?.message || e));
    } finally {
      this.testing.set(false);
    }
  }

  async runNow() {
    this.translating.set(true);
    this.actionMessage.set('');
    this.actionError.set('');
    try {
      await this.saveTranslation();
      const queued = await this.translationService.runNow(Number(this.settings().translate_batch_size) || 10);
      this.actionMessage.set(this.i18n.t(queued ? 'trans.jobStarted' : 'trans.jobRunning'));
    } catch (e: any) {
      this.actionError.set(String(e?.message || e));
    } finally {
      this.translating.set(false);
      await this.refreshTranslation();
    }
  }

  async resetFailed() {
    this.actionError.set('');
    try {
      const n = await this.translationService.resetFailed();
      this.actionMessage.set(`${n}`);
      await this.refreshTranslation();
    } catch (e: any) {
      this.actionError.set(String(e?.message || e));
    }
  }

  async refreshTranslation() {
    try {
      const [st, stats] = await Promise.all([
        this.translationService.getStatus(),
        this.translationService.getStats()
      ]);
      this.status.set(st);
      this.stats.set(stats);
    } catch (e) {
      console.error('Failed to load translation status:', e);
    }
    // Keep polling while the page is open: fast during a run so the six
    // counters track progress live, slower when idle so the background
    // worker's translations still show up without hammering the backend.
    const running = !!this.status()?.isRunning;
    if (running !== this.pollFast) {
      this.pollFast = running;
      this.startPolling();
    }
    // Cheap counters for the audit buttons; also detects background job
    // completion so the tables refresh themselves.
    await this.refreshCounts();
  }

  /** Polls audit counters and the shared LLM queue; refreshes the audit
   *  tables when a classification job finishes. */
  private async refreshCounts() {
    let c: AuditCounts;
    try {
      c = await this.translationService.auditCounts();
    } catch (e) {
      console.error('Failed to load audit counts:', e);
      return;
    }
    this.counts.set(c);
    let q: QueueStatus | null = null;
    try {
      q = await this.translationService.queueStatus();
    } catch (e) {
      console.error('Failed to load queue status:', e);
    }
    if (q) {
      this.queue.set(q);
    }
    const classifyJob = q?.current && q.current.kind === 'classify' ? q.current.name : '';
    if (this.prevClassifyJob && !classifyJob) {
      // A classification job just finished: the audit tables changed.
      this.prevClassifyJob = '';
      await this.runAudit();
      return;
    }
    this.prevClassifyJob = classifyJob;
  }

  async runAudit() {
    this.auditing.set(true);
    this.auditMessage.set('');
    this.auditError.set('');
    const t0 = Date.now();
    try {
      const a = await this.translationService.auditCategories();
      this.audit.set(a);
      // The table below rarely changes between runs, so the elapsed time in
      // the message is what proves the click actually did something.
      this.auditMessage.set(
        `${this.i18n.t('trans.auditDone')} · ${Date.now() - t0}ms · ` +
        `${this.i18n.t('trans.auditTotal')}: ${a.counts.total} · ` +
        `${this.i18n.t('trans.auditWeak')}: ${a.suspects.length}`
      );
    } catch (e: any) {
      this.auditError.set(String(e?.message || e));
    } finally {
      this.auditing.set(false);
    }
  }

  async classifyUnclassified() {
    this.classifyingUnclassified.set(true);
    this.auditError.set('');
    this.auditMessage.set('');
    try {
      const started = await this.translationService.auditClassifyUnclassified();
      this.auditMessage.set(this.i18n.t(started ? 'trans.jobStarted' : 'trans.jobRunning'));
      await this.refreshCounts();
    } catch (e: any) {
      this.auditError.set(String(e?.message || e));
    } finally {
      this.classifyingUnclassified.set(false);
    }
  }

  async reclassifySuspects() {
    this.reclassifyingSuspects.set(true);
    this.auditError.set('');
    this.auditMessage.set('');
    try {
      const started = await this.translationService.auditReclassifySuspects();
      this.auditMessage.set(this.i18n.t(started ? 'trans.jobStarted' : 'trans.jobRunning'));
      await this.refreshCounts();
    } catch (e: any) {
      this.auditError.set(String(e?.message || e));
    } finally {
      this.reclassifyingSuspects.set(false);
    }
  }

  async cancelTranslate() {
    this.actionError.set('');
    try {
      await this.translationService.cancelTranslate();
      this.actionMessage.set(this.i18n.t('trans.cancelled'));
      await this.refreshCounts();
    } catch (e: any) {
      this.actionError.set(String(e?.message || e));
    }
  }

  async cancelClassify() {
    this.auditError.set('');
    try {
      await this.translationService.cancelClassify();
      this.auditMessage.set(this.i18n.t('trans.cancelled'));
      await this.refreshCounts();
    } catch (e: any) {
      this.auditError.set(String(e?.message || e));
    }
  }

  async cancelAll() {
    this.auditError.set('');
    try {
      await this.translationService.cancelAll();
      this.auditMessage.set(this.i18n.t('trans.cancelled'));
      await this.refreshCounts();
    } catch (e: any) {
      this.auditError.set(String(e?.message || e));
    }
  }

  /** Short human label for the queue status line. */
  jobLabel(job: QueueJob): string {
    if (job.kind === 'translate') {
      return job.name === 'batch' ? this.i18n.t('trans.jobBatch') : this.i18n.t('trans.jobSingle');
    }
    if (job.name === 'audit-reclassify') {
      return this.i18n.t('trans.reclassifySuspects');
    }
    return this.i18n.t('trans.classifyUnclassified');
  }

  /** Persists the classifier's own model settings (independent of translation). */
  async saveClassifier() {
    const s = this.settings();
    this.clsMessage.set('');
    this.clsError.set('');
    try {
      for (const k of ['cls_base_url', 'cls_model', 'cls_api_key'] as (keyof SettingsMap)[]) {
        await this.settingService.set(k, s[k]);
      }
      this.clsMessage.set(this.i18n.t('settings.saved'));
    } catch (e: any) {
      this.clsError.set(String(e?.message || e));
    }
  }

  /** Saves cls_* first (the backend reads them from the database), then pings the model. */
  async testClassifier() {
    this.clsTesting.set(true);
    this.clsMessage.set('');
    this.clsError.set('');
    try {
      await this.saveClassifier();
      this.clsMessage.set(await this.translationService.testClassifier());
    } catch (e: any) {
      this.clsError.set(String(e?.message || e));
    } finally {
      this.clsTesting.set(false);
    }
  }

  /** Persists the tts_* keys (the backend reads them from the database). */
  async saveTts() {
    const s = this.settings();
    this.ttsMessage.set('');
    this.ttsError.set('');
    try {
      const keys: (keyof SettingsMap)[] = [
        'tts_enabled', 'tts_engine', 'tts_voice', 'tts_max_per_round', 'tts_concurrency',
        'tts_auto_play', 'tts_python',
        'tts_api_url', 'tts_api_key', 'tts_api_model', 'tts_api_voice',
      ];
      for (const k of keys) {
        await this.settingService.set(k, s[k]);
      }
      this.ttsMessage.set(this.i18n.t('settings.saved'));
    } catch (e: any) {
      this.ttsError.set(String(e?.message || e));
    }
  }

  /** Saves tts_* first, then synthesises a fixed sentence as an audible check. */
  async testTts() {
    this.ttsTesting.set(true);
    this.ttsMessage.set('');
    this.ttsError.set('');
    this.ttsAudio.set('');
    try {
      await this.saveTts();
      const url = await this.playback.test();
      this.ttsAudio.set(url);
      this.ttsMessage.set(this.i18n.t('tts.ok'));
    } catch (e: any) {
      this.ttsError.set(String(e?.message || e));
    } finally {
      this.ttsTesting.set(false);
    }
  }

  async saveSettings() {
    this.generalMessage.set('');
    this.generalError.set('');
    const s = this.settings();
    const keys: (keyof SettingsMap)[] = [
      'max_concurrency', 'default_interval_seconds', 'article_ttl_days', 'log_level',
      'compliance_filter_enabled', 'robots_enforcement', 'auto_collect',
      'cls_base_url', 'cls_model', 'cls_api_key',
    ];
    try {
      for (const k of keys) {
        await this.settingService.set(k, s[k]);
      }
      await this.saveTranslation();
      // saveTranslation posts its own "saved" message in the translation
      // section; this section shows its own, right under this button.
      this.actionMessage.set('');
      // Re-read from DB to ensure the signal matches persisted state — prevents
      // the value from reverting if the user navigates away before the async
      // saves fully settle (race between save and loadSettings on re-entry).
      await this.loadSettings();
      this.generalMessage.set(this.i18n.t('settings.saved'));
    } catch (e: any) {
      this.generalError.set(String(e?.message || e));
    }
  }
}
