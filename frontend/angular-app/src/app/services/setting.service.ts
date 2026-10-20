import { Injectable } from '@angular/core';
import { SettingGetAll, SettingSet } from '../../../wailsjs/go/bindings/Bindings';

/** Anything the settings UI can hold; numbers are stringified before the Go call. */
export type SettingValue = string | number | boolean | null | undefined;

@Injectable({ providedIn: 'root' })
export class SettingService {
  async getAll(): Promise<Record<string, string>> {
    return await SettingGetAll();
  }

  async set(key: string, value: SettingValue): Promise<void> {
    // Go binds this parameter as a string. Numeric values coming from
    // <input type="number"> would otherwise fail with
    // "error parsing arguments: json: cannot unmarshal number into Go value of type string".
    await SettingSet(key, value === null || value === undefined ? '' : String(value));
  }
}
