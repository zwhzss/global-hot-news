import { Component } from '@angular/core';
import { Router } from '@angular/router';
import { I18nService } from '../../services/i18n.service';

/**
 * Left navigation.
 *
 * Every label goes through the i18n service, and the header carries the language
 * switch that flips the whole application (labels, category names, empty states,
 * dates and the default content language of every article card).
 */
@Component({
  selector: 'sidebar',
  standalone: false,
  template: `
    <div class="sidebar">
      <div class="sidebar-header">
        <h1>Global Hot News</h1>
        <div class="lang-switch">
          <button
            class="lang-switch-btn"
            [class.active]="i18n.lang() === 'zh'"
            (click)="i18n.setLang('zh')"
          >中文</button>
          <button
            class="lang-switch-btn"
            [class.active]="i18n.lang() === 'en'"
            (click)="i18n.setLang('en')"
          >English</button>
        </div>
      </div>
      <nav class="sidebar-nav">
        <div class="nav-item" routerLinkActive="active" [routerLinkActiveOptions]="{exact: true}" routerLink="/recommend">
          <span class="icon">&#9733;</span> {{ i18n.t('nav.recommend') }}
        </div>
        <div class="nav-item" routerLinkActive="active" routerLink="/followed">
          <span class="icon">&#9734;</span> {{ i18n.t('nav.followed') }}
        </div>
        <div class="nav-item" routerLinkActive="active" routerLink="/hotrank">
          <span class="icon">&#9650;</span> {{ i18n.t('nav.hotrank') }}
        </div>
        <div class="nav-item" routerLinkActive="active" routerLink="/finance">
          <span class="icon">&#36;</span> {{ i18n.t('nav.finance') }}
        </div>
        <div class="nav-item" routerLinkActive="active" routerLink="/military">
          <span class="icon">&#9876;</span> {{ i18n.t('nav.military') }}
        </div>
        <div class="nav-item" routerLinkActive="active" routerLink="/tech">
          <span class="icon">&#9881;</span> {{ i18n.t('nav.tech') }}
        </div>
        <div class="nav-item" routerLinkActive="active" routerLink="/politics">
          <span class="icon">&#9878;</span> {{ i18n.t('nav.politics') }}
        </div>
        <div class="nav-item" routerLinkActive="active" routerLink="/economy">
          <span class="icon">&#128200;</span> {{ i18n.t('nav.economy') }}
        </div>
        <div class="nav-item" routerLinkActive="active" routerLink="/datasources">
          <span class="icon">&#9883;</span> {{ i18n.t('nav.datasources') }}
        </div>
        <div class="nav-item" routerLinkActive="active" routerLink="/tasks">
          <span class="icon">&#128203;</span> {{ i18n.t('nav.tasks') }}
        </div>
        <div class="nav-item" routerLinkActive="active" routerLink="/settings">
          <span class="icon">&#9881;</span> {{ i18n.t('nav.settings') }}
        </div>
      </nav>
    </div>
  `,
})
export class SidebarComponent {
  constructor(public router: Router, public i18n: I18nService) {}
}
