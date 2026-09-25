import { api } from '$lib/api';
import type { PageLoad } from './$types';

export type Check = { can_delete: boolean; blockers: string[]; email: string };

export const load: PageLoad = async ({ fetch }) => {
	try {
		return { check: await api<Check>('/api/v1/me/deletion', undefined, fetch), loadError: '' };
	} catch (e) {
		return {
			check: null as Check | null,
			loadError: e instanceof Error ? e.message : 'Your account could not be checked.'
		};
	}
};
