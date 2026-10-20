import { NgModule, provideBrowserGlobalErrorListeners } from '@angular/core';
import { BrowserModule } from '@angular/platform-browser';
import { FormsModule } from '@angular/forms';
import { AppRoutingModule } from './app-routing-module';
import { App } from './app';
import { NewsService } from './services/news.service';
import { DataSourceService } from './services/datasource.service';
import { FollowService } from './services/follow.service';
import { RecommendService } from './services/recommend.service';
import { HotRankService } from './services/hotrank.service';
import { SettingService } from './services/setting.service';
import { I18nService } from './services/i18n.service';
import { TranslationService } from './services/translation.service';
import { SidebarComponent } from './components/sidebar/sidebar.component';
import { NewsCardComponent } from './components/news-card/news-card.component';
import { NewsFilterComponent } from './components/news-filter/news-filter.component';
import { ArticleBoardComponent } from './components/article-board/article-board.component';
import { TopicBoardComponent } from './components/topic-board/topic-board.component';
import { PlayerBarComponent } from './components/player-bar/player-bar.component';
import { RecommendPage } from './pages/recommend/recommend.page';
import { FollowedPage } from './pages/followed/followed.page';
import { HotRankPage } from './pages/hotrank/hotrank.page';
import { FinancePage } from './pages/finance/finance.page';
import { MilitaryPage } from './pages/military/military.page';
import { TechPage } from './pages/tech/tech.page';
import { PoliticsPage } from './pages/politics/politics.page';
import { EconomyPage } from './pages/economy/economy.page';
import { DataSourcePage } from './pages/datasource/datasource.page';
import { SettingsPage } from './pages/settings/settings.page';
import { TasksPage } from './pages/tasks/tasks.page';

@NgModule({
  declarations: [
    App,
    SidebarComponent,
    NewsCardComponent,
    NewsFilterComponent,
    ArticleBoardComponent,
    TopicBoardComponent,
    PlayerBarComponent,
    RecommendPage,
    FollowedPage,
    HotRankPage,
    FinancePage,
    MilitaryPage,
    TechPage,
    PoliticsPage,
    EconomyPage,
    DataSourcePage,
    SettingsPage,
    TasksPage,
  ],
  imports: [
    BrowserModule,
    AppRoutingModule,
    FormsModule,
  ],
  providers: [
    provideBrowserGlobalErrorListeners(),
    NewsService,
    DataSourceService,
    FollowService,
    RecommendService,
    HotRankService,
    SettingService,
    I18nService,
    TranslationService,
  ],
  bootstrap: [App],
})
export class AppModule {}
