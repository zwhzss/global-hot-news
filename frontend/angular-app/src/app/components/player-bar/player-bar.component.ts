import { Component } from '@angular/core';
import { I18nService } from '../../services/i18n.service';
import { PlaybackService } from '../../services/playback.service';

/**
 * Floating transport bar: appears once a round's playlist exists (or a round
 * is still synthesising), and stays for the whole session — navigating away
 * from the hot-rank / recommendation boards keeps the audio running.
 */
@Component({
  selector: 'app-player-bar',
  standalone: false,
  template: `
    @if (pb.queue().length > 0 || pb.status()?.state === 'synthesizing') {
      <div class="player-bar">
        <div class="player-controls">
          <button class="player-btn" (click)="pb.prev()" [disabled]="pb.queue().length === 0" title="prev">&#9198;</button>
          <button class="player-btn player-btn-main" (click)="pb.toggle()" [disabled]="pb.queue().length === 0">
            {{ pb.playing() ? '&#9208;' : '&#9654;' }}
          </button>
          <button class="player-btn" (click)="pb.next()" [disabled]="pb.queue().length === 0" title="next">&#9197;</button>
        </div>

        <div class="player-body">
          @if (pb.current(); as cur) {
            <div class="player-meta">
              <span class="player-title" [title]="cur.title">{{ cur.title }}</span>
              @if (cur.source) {
                <span class="player-source">{{ cur.source }}</span>
              }
            </div>
            <div class="player-progress" (click)="seek($event)">
              <div class="player-progress-fill" [style.width.%]="pb.progress() * 100"></div>
            </div>
          } @else {
            <div class="player-meta">
              <span class="player-title text-secondary">{{ i18n.t('player.noRound') }}</span>
            </div>
            <div class="player-progress"><div class="player-progress-fill" style="width:0%"></div></div>
          }
        </div>

        <div class="player-side">
          @if (pb.blocked() && !pb.playing()) {
            <span class="player-hint">{{ i18n.t('player.blocked') }}</span>
          }
          @if (pb.status()?.state === 'synthesizing') {
            <span class="player-hint">
              {{ i18n.t('player.synthesizing') }} {{ pb.status()?.done }}/{{ pb.status()?.total }}
            </span>
          } @else if (pb.queue().length > 0) {
            <span class="player-count">{{ pb.index() + 1 }} / {{ pb.queue().length }}</span>
          }
        </div>
      </div>
    }
  `,
})
export class PlayerBarComponent {
  constructor(
    public i18n: I18nService,
    public pb: PlaybackService
  ) {}

  seek(ev: MouseEvent) {
    const el = ev.currentTarget as HTMLElement;
    if (!el.clientWidth) return;
    this.pb.seek(ev.offsetX / el.clientWidth);
  }
}
