import { api } from '$lib/api';
import type { PageLoad } from './$types';

export type Session = { current: boolean; created_at: string; expires_at: string; operations_verified: boolean };

export const load: PageLoad = async ({ fetch }) => {
	try {
		return {
			sessions: (await api<{ sessions: Session[] }>('/api/v1/me/sessions', undefined, fetch)).sessions,
			loadError: ''
		};
	} catch (e) {
		return { sessions: [] as Session[], loadError: e instanceof Error ? e.message : 'Sessions could not be loaded.' };
	}
};
