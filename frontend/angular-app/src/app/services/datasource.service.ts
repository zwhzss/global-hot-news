import { Injectable, signal } from '@angular/core';
import { DataSourceCreate, DataSourceDelete, DataSourceFetchNow, DataSourceList, DataSourceTest, DataSourceToggleActive, DataSourceUpdate } from '../../../wailsjs/go/bindings/Bindings';
import { models } from '../../../wailsjs/go/models';

export interface TestResult {
  available: boolean;
  message: string;
  samples?: any[];
}

export interface Source {
  id: number;
  name: string;
  type: string;
  url: string;
  isActive: boolean;
  cronExpression: string;
  requestInterval: number;
  publisher: string;
  status: string;
  lastError: string;
  lastFetchedAt?: string;
  fetchCount: number;
  createdAt: string;
  updatedAt: string;
}

function toSource(s: models.Source): Source {
  return {
    id: s.ID,
    name: s.name,
    type: s.type,
    url: s.url,
    isActive: s.isActive,
    cronExpression: s.cronExpression,
    requestInterval: s.requestInterval,
    publisher: s.publisher || '',
    status: s.status,
    lastError: s.lastError,
    lastFetchedAt: s.lastFetchedAt ? new Date(s.lastFetchedAt).toISOString() : undefined,
    fetchCount: s.fetchCount,
    createdAt: s.createdAt ? new Date(s.createdAt).toISOString() : '',
    updatedAt: s.updatedAt ? new Date(s.updatedAt).toISOString() : '',
  };
}

@Injectable({ providedIn: 'root' })
export class DataSourceService {
  private sources = signal<Source[]>([]);
  sources$ = this.sources.asReadonly();

  async list(): Promise<Source[]> {
    try {
      if (!(window as any).go?.bindings?.Bindings) {
        console.warn('Wails bindings not available, retrying...');
        // Wait a bit and try again
        await new Promise(resolve => setTimeout(resolve, 1000));
        if (!(window as any).go?.bindings?.Bindings) {
          throw new Error('Wails bindings still not available');
        }
      }
      
      const result = await Promise.race([
        DataSourceList(),
        new Promise<any[]>((_, reject) => setTimeout(() => reject(new Error('DataSourceList timeout')), 10000)),
      ]);
      const sources = (result || []).map(toSource);
      this.sources.set(sources);
      return sources;
    } catch (error) {
      console.error('Failed to load sources:', error);
      // Return empty array but keep trying in background
      this.sources.set([]);
      return [];
    }
  }

  async create(name: string, type: string, url: string, cronExpr: string, interval: number, publisher: string = ''): Promise<Source> {
    const result = await Promise.race([
      DataSourceCreate(name, type, url, cronExpr, interval),
      new Promise<any>((_, reject) => setTimeout(() => reject(new Error('DataSourceCreate timeout')), 10000)),
    ]);
    // Set publisher after create (binding doesn't support it yet)
    if (publisher) {
      await this.setPublisher((result as any).ID, publisher);
    }
    await this.list();
    return toSource(result);
  }

  async update(id: number, name: string, type: string, url: string, interval: number, publisher: string = ''): Promise<void> {
    await Promise.race([
      DataSourceUpdate(id, name, type, url, interval),
      new Promise<void>((_, reject) => setTimeout(() => reject(new Error('DataSourceUpdate timeout')), 10000)),
    ]);
    if (publisher) {
      await this.setPublisher(id, publisher);
    }
    await this.list();
  }

  async setPublisher(id: number, publisher: string): Promise<void> {
    // Import the generated binding after rebuild. For now use a direct call.
    const bindings = (window as any).go?.bindings?.Bindings;
    if (bindings?.DataSourceSetPublisher) {
      await Promise.race([
        bindings.DataSourceSetPublisher(id, publisher),
        new Promise<void>((_, reject) => setTimeout(() => reject(new Error('DataSourceSetPublisher timeout')), 10000)),
      ]);
    }
  }

  async delete(id: number): Promise<void> {
    await Promise.race([
      DataSourceDelete(id),
      new Promise<void>((_, reject) => setTimeout(() => reject(new Error('DataSourceDelete timeout')), 10000)),
    ]);
    await this.list();
  }

  async toggleActive(id: number, active: boolean): Promise<void> {
    await Promise.race([
      DataSourceToggleActive(id, active),
      new Promise<void>((_, reject) => setTimeout(() => reject(new Error('DataSourceToggleActive timeout')), 10000)),
    ]);
    await this.list();
  }

  async test(id: number): Promise<any> {
    return await Promise.race([
      DataSourceTest(id),
      new Promise<any>((_, reject) => setTimeout(() => reject(new Error('DataSourceTest timeout')), 30000)),
    ]);
  }

  async fetchNow(id: number): Promise<number> {
    return await Promise.race([
      DataSourceFetchNow(id),
      new Promise<number>((_, reject) => setTimeout(() => reject(new Error('DataSourceFetchNow timeout')), 60000)),
    ]);
  }
}
