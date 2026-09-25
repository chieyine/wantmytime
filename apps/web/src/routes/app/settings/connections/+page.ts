import { api } from '$lib/api';
import type { PageLoad } from './$types';

export type Calendar = {
	configured: boolean;
	connected: boolean;
	account_email?: string;
	check_busy?: boolean;
	add_events?: boolean;
	create_meet_links?: boolean;
	status?: 'active' | 'error' | 'revoked';
	last_error?: string | null;
	last_synced_at?: string | null;
};

export const load: PageLoad = async ({ fetch }) => {
	try {
		return { calendar: await api<Calendar>('/api/v1/me/calendar', undefined, fetch), loadError: '' };
	} catch (e) {
		return {
			calendar: null as Calendar | null,
			loadError: e instanceof Error ? e.message : 'Calendar status could not be loaded.'
		};
	}
};
