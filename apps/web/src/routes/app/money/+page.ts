import { api } from '$lib/api';
import type { Payout } from '$lib/payouts';
import type { PageLoad } from './$types';

type Payouts = { payouts: Payout[]; upcoming_minor: number; paid_minor: number; paused: boolean; currency?: string };

export const load: PageLoad = async ({ fetch }) => {
	try {
		const [payouts, account] = await Promise.all([
			api<Payouts>('/api/v1/me/payouts', undefined, fetch),
			api<{ account: unknown | null }>('/api/v1/me/payout-account', undefined, fetch)
		]);
		return { payouts, hasBank: account.account !== null, loadError: '' };
	} catch (e) {
		return {
			payouts: null as Payouts | null,
			hasBank: null as boolean | null,
			loadError: e instanceof Error ? e.message : 'Your money could not be loaded.'
		};
	}
};
