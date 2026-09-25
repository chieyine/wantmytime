import { api } from '$lib/api';
import type { PageLoad } from './$types';

export type Account = {
	bank_name: string;
	account_last4: string;
	account_name: string;
	verified_at: string;
	usable_from: string;
	in_safety_hold: boolean;
	type?: string;
	country?: string;
	currency?: string;
};
export type Operator = { code: string; name: string };
export type Market = {
	country: string;
	name: string;
	currency: string;
	account_digits: number;
	bank_payouts: boolean;
	name_check: boolean;
	mobile_money: Operator[] | null;
	phone_prefix?: string;
};
type PayoutAccount = {
	account: Account | null;
	payout_delay_minutes: number;
	dispute_window_minutes: number;
	market?: Market;
};

export const load: PageLoad = async ({ fetch }) => {
	try {
		const [payoutAccount, payouts] = await Promise.all([
			api<PayoutAccount>('/api/v1/me/payout-account', undefined, fetch),
			api<{ paused: boolean }>('/api/v1/me/payouts', undefined, fetch)
		]);
		return { payoutAccount, paused: payouts.paused, loadError: '' };
	} catch (e) {
		return {
			payoutAccount: null as PayoutAccount | null,
			paused: false,
			loadError: e instanceof Error ? e.message : 'Payouts could not be loaded.'
		};
	}
};
