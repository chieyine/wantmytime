import { api } from '$lib/api';
import type { PageLoad } from './$types';

export type Booking = {
	id: string;
	seller: string;
	buyer: string;
	duration_minutes: number;
	starts_at: string;
	state: string;
	payment_state: string;
};
export type Offer = {
	id: string;
	seller: string;
	buyer_name: string;
	amount_minor: string;
	state: string;
	role: 'seller' | 'buyer';
	expires_at: string;
	currency?: string;
};

// Each part of the overview loads on its own, so one slow or failing call
// leaves the rest of the page intact.
export const load: PageLoad = async ({ fetch, parent }) => {
	const { account } = await parent();
	if (!(account as { handle?: string }).handle) return { overview: null };
	const [link, availability, bookings, offers, payoutAccount, payouts] = await Promise.allSettled([
		api<{ ready: boolean; paused: boolean; currency?: string }>('/api/v1/me/link', undefined, fetch),
		api<{ windows: unknown[] }>('/api/v1/me/availability', undefined, fetch),
		api<{ bookings: Booking[] }>('/api/v1/me/bookings', undefined, fetch),
		api<{ offers: Offer[] }>('/api/v1/me/offers', undefined, fetch),
		api<{ account: unknown | null }>('/api/v1/me/payout-account', undefined, fetch),
		api<{ upcoming_minor: number }>('/api/v1/me/payouts', undefined, fetch)
	]);
	const value = <T>(result: PromiseSettledResult<T>) => (result.status === 'fulfilled' ? result.value : null);
	return {
		overview: {
			profileReady: value(link)?.ready ?? null,
			profilePaused: value(link)?.paused ?? false,
			currency: value(link)?.currency || 'NGN',
			windowCount: value(availability)?.windows.length ?? null,
			bookings: value(bookings)?.bookings ?? [],
			offers: value(offers)?.offers ?? [],
			hasBank: payoutAccount.status === 'fulfilled' ? payoutAccount.value.account !== null : null,
			onTheWay: value(payouts)?.upcoming_minor ?? null,
			failed: [link, availability, bookings, offers, payoutAccount, payouts].some((r) => r.status === 'rejected')
		}
	};
};
