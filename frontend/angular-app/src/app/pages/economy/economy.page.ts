import { Component, OnDestroy, OnInit } from '@angular/core';
import { I18nService } from '../../services/i18n.service';
import { PlaybackService } from '../../services/playback.service';

/** Economy board. See FinancePage for why these pages are one-liners. */
@Component({
  selector: 'app-economy',
  standalone: false,
  template: `
    <div class="page-header flex flex-between">
      <h2>{{ i18n.t('nav.economy') }}</h2>
      <div class="flex" style="gap: 8px; align-items: center;">
        <button class="btn" (click)="playback.playRound()">{{ i18n.t('player.play') }}</button>
        <button class="btn" (click)="playback.pause()">{{ i18n.t('player.pause') }}</button>
        <button class="btn" (click)="playback.stop()">{{ i18n.t('player.stop') }}</button>
      </div>
    </div>
    <div class="page-content">
      <topic-board prefix="economy" emptyKey="empty.economy" icon="&#128200;"></topic-board>
    </div>
  `,
})
export class EconomyPage implements OnInit, OnDestroy {
  constructor(
    public i18n: I18nService,
    public playback: PlaybackService
  ) {}

  ngOnInit() {
    this.playback.startWatch();
  }

  ngOnDestroy() {
    this.playback.stopWatch();
  }
}
