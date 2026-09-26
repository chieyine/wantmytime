import { api } from '$lib/api';
import type { Calendar } from './connections/+page';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const [marketing, calendar] = await Promise.all([
		api<{ subscribed: boolean }>('/api/v1/me/marketing', undefined, fetch).catch(() => null),
		api<Calendar>('/api/v1/me/calendar', undefined, fetch).catch(() => null)
	]);
	return { marketing, calendar };
};
