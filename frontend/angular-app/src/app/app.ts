import { Component, OnInit } from '@angular/core';
import { RouterOutlet } from '@angular/router';
import { SidebarComponent } from './components/sidebar/sidebar.component';
import { I18nService } from './services/i18n.service';

@Component({
  selector: 'app-root',
  standalone: false,
  template: `
    <div class="app-layout">
      <sidebar></sidebar>
      <main class="main-content">
        <router-outlet></router-outlet>
      </main>
      <app-player-bar></app-player-bar>
    </div>
  `,
  styleUrls: ['./app.css'],
})
export class App implements OnInit {
  constructor(private i18n: I18nService) {}

  async ngOnInit() {
    // The stored UI language is the source of truth on startup, so the choice
    // survives a reinstall (or a wiped WebView2 profile) instead of living only
    // in browser storage.
    await this.i18n.load();
  }
}
