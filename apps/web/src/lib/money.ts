// Money is always integer minor units (kobo, pesewas, cents) plus an ISO
// currency code. Sellers are priced and paid in their own country's currency.

const symbols: Record<string, string> = { NGN: '₦', GHS: 'GH₵', KES: 'KSh ', ZAR: 'R', USD: '$', GBP: '£', EUR: '€' };

/** The symbol shown before a price, e.g. ₦ or KSh. Unknown codes show the code. */
export function currencySymbol(currency = 'NGN'): string {
	const code = (currency || 'NGN').trim().toUpperCase();
	return symbols[code] ?? `${code} `;
}

function toMinor(minor: number | string | bigint | null | undefined): bigint {
	try {
		return typeof minor === 'bigint' ? minor : typeof minor === 'string' ? BigInt(minor.trim() || '0') : BigInt(Math.trunc(Number.isFinite(minor as number) ? (minor as number) : 0));
	} catch {
		return 0n;
	}
}

/** 1050000 NGN -> ₦10,500; 1050 -> ₦10.50. */
export function formatMoney(minor: number | string | bigint | null | undefined, currency = 'NGN'): string {
	const value = toMinor(minor);
	const negative = value < 0n;
	const abs = negative ? -value : value;
	const whole = abs / 100n;
	const fraction = abs % 100n;
	return `${negative ? '-' : ''}${currencySymbol(currency)}${new Intl.NumberFormat('en').format(whole)}${fraction ? `.${fraction.toString().padStart(2, '0')}` : ''}`;
}

/** Kept for pages that only ever show naira (operations totals and the like). */
export function formatNaira(minor: number | string | bigint | null | undefined): string {
	return formatMoney(minor, 'NGN');
}

/** "10,500.5" (any symbol or spaces) -> 1050050n; null when it isn't an amount. */
export function parseMoneyToMinor(input: string): bigint | null {
	const clean = input.replace(/[^\d.]/g, '');
	if (!/^\d+(?:\.\d{1,2})?$/.test(clean)) return null;
	const [whole, decimal = ''] = clean.split('.');
	return BigInt(whole) * 100n + BigInt(decimal.padEnd(2, '0'));
}

export const parseNairaToMinor = parseMoneyToMinor;

// Mirrors the API's quote rule: round(base_30 * minutes / 30), halves rounded up.
export function priceForDuration(base30Minor: number | string, minutes: number): number {
	const raw = typeof base30Minor === 'string' ? Number(base30Minor) : base30Minor;
	if (!Number.isFinite(raw) || raw < 0) return 0;
	const base = BigInt(Math.trunc(raw));
	return Number((base * BigInt(minutes) + 15n) / 30n);
}

/** How each way of paying is named to buyers. */
export const methodLabels: Record<string, string> = {
	bank_transfer: 'Bank transfer',
	pay_with_bank: 'Pay with your bank app',
	mobile_money: 'Mobile money'
};

/** One line saying how buyers pay, for the profile and booking pages. */
export function paymentMethodsSentence(methods: string[] | undefined): string {
	const list = (methods ?? []).map((m) => (methodLabels[m] ?? m).toLowerCase());
	if (list.length === 0) return 'You pay after you pick a time.';
	const joined = list.length === 1 ? list[0] : `${list.slice(0, -1).join(', ')} or ${list[list.length - 1]}`;
	return `You pay by ${joined} after you pick a time.`;
}
