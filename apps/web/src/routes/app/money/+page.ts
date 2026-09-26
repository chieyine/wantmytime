import { api } from '$lib/api';
import type { Payout } from '$lib/payouts';
import type { Account } from './payouts/+page';
import type { PageLoad } from './$types';

type Payouts = { payouts: Payout[]; upcoming_minor: number; paid_minor: number; paused: boolean; currency?: string };
type Owed = { outstanding_minor: number; max_share_bps: number; currency?: string };

export const load: PageLoad = async ({ fetch }) => {
	try {
		const [payouts, account, owed] = await Promise.all([
			api<Payouts>('/api/v1/me/payouts', undefined, fetch),
			api<{ account: Account | null }>('/api/v1/me/payout-account', undefined, fetch),
			api<Owed>('/api/v1/me/refund-recoveries', undefined, fetch).catch(() => null)
		]);
		return { payouts, account: account.account, hasBank: account.account !== null, owed, loadError: '' };
	} catch (e) {
		return {
			payouts: null as Payouts | null,
			account: null as Account | null,
			hasBank: null as boolean | null,
			owed: null as Owed | null,
			loadError: e instanceof Error ? e.message : 'Your money could not be loaded.'
		};
	}
};
