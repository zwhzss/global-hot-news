import { NgModule } from '@angular/core';
import { RouterModule, Routes } from '@angular/router';
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

const routes: Routes = [
  { path: '', redirectTo: 'recommend', pathMatch: 'full' },
  { path: 'recommend', component: RecommendPage },
  { path: 'followed', component: FollowedPage },
  { path: 'hotrank', component: HotRankPage },
  { path: 'finance', component: FinancePage },
  { path: 'military', component: MilitaryPage },
  { path: 'tech', component: TechPage },
  { path: 'politics', component: PoliticsPage },
  { path: 'economy', component: EconomyPage },
  { path: 'datasources', component: DataSourcePage },
  { path: 'tasks', component: TasksPage },
  { path: 'settings', component: SettingsPage },
  { path: '**', redirectTo: 'recommend' },
];

@NgModule({
  imports: [RouterModule.forRoot(routes)],
  exports: [RouterModule],
})
export class AppRoutingModule {}
