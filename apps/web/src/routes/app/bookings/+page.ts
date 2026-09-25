import { api } from '$lib/api';
import type { PageLoad } from './$types';

export type Booking = {
	id: string;
	seller: string;
	buyer: string;
	duration_minutes: number;
	starts_at: string;
	amount_minor: number;
	state: string;
	payment_state: string;
	currency?: string;
};

export const load: PageLoad = async ({ fetch }) => {
	try {
		const result = await api<{ bookings: Booking[]; next_cursor: string }>('/api/v1/me/bookings', undefined, fetch);
		return { bookings: result.bookings, nextCursor: result.next_cursor, loadError: '' };
	} catch (error) {
		return {
			bookings: [] as Booking[],
			nextCursor: '',
			loadError: error instanceof Error ? error.message : 'Bookings could not be loaded.'
		};
	}
};
