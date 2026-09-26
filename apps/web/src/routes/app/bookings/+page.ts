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

export type Offer = {
	id: string;
	seller: string;
	buyer_name: string;
	duration_minutes: number;
	state: string;
	version: number;
	expires_at: string;
	amount_minor: string;
	role: 'seller' | 'buyer';
	currency?: string;
};

// Bookings and price requests (offers) live on one page; each loads on its own.
export const load: PageLoad = async ({ fetch }) => {
	const [bookings, offers] = await Promise.allSettled([
		api<{ bookings: Booking[]; next_cursor: string }>('/api/v1/me/bookings', undefined, fetch),
		api<{ offers: Offer[] }>('/api/v1/me/offers', undefined, fetch)
	]);
	return {
		bookings: bookings.status === 'fulfilled' ? bookings.value.bookings : ([] as Booking[]),
		nextCursor: bookings.status === 'fulfilled' ? bookings.value.next_cursor : '',
		offers: offers.status === 'fulfilled' ? offers.value.offers : ([] as Offer[]),
		loadError:
			bookings.status === 'rejected'
				? bookings.reason instanceof Error
					? bookings.reason.message
					: 'Bookings could not be loaded.'
				: ''
	};
};
