import { resolve } from 'node:path';

export type E2EState = { apiPid: number; adminUrl: string; dbName: string; logPath: string };

export const stateDir = resolve(import.meta.dirname, '../../.e2e');
export const statePath = resolve(stateDir, 'state.json');
