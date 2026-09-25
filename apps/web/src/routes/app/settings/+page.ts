import { api } from '$lib/api';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	try {
		return { marketing: await api<{ subscribed: boolean }>('/api/v1/me/marketing', undefined, fetch) };
	} catch {
		return { marketing: null };
	}
};
