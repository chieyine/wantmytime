import { api } from '$lib/api';
import type { PageLoad } from './$types';

export type WindowRule = { weekday: number; start: string; end: string };
export type Override = { date: string; closed: boolean };
export type Availability = {
	windows: WindowRule[];
	overrides: Override[];
	timezone: string;
	minimum_notice_minutes: number;
	booking_horizon_days: number;
	buffer_minutes: number;
};

export const load: PageLoad = async ({ fetch }) => {
	try {
		return { availability: await api<Availability>('/api/v1/me/availability', undefined, fetch), loadError: '' };
	} catch (error) {
		return {
			availability: null as Availability | null,
			loadError: error instanceof Error ? error.message : 'Availability could not be loaded.'
		};
	}
};
