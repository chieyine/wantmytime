// Shared wording for payouts on the Money and Payouts pages.
export type Payout = { id: string; booking_id: string; buyer_name: string; state: string; entitlement_minor: number; amount_minor: number | null; transfer_minor?: number; recovery_minor: number; release_at: string; paid_at: string | null; bank_name: string; account_last4: string; hold: string; hold_text?: string; note?: string };

export const when = (v: string) => new Date(v).toLocaleString(undefined, { weekday: 'short', day: 'numeric', month: 'short', hour: 'numeric', minute: '2-digit' });

export function payoutStatus(p: Payout): { label: string; detail: string; tone: string } {
	switch (p.state) {
		case 'paid':
			return { label: 'Paid', detail: `Sent ${p.paid_at ? when(p.paid_at) : ''} to ${p.bank_name} ••${p.account_last4}`, tone: '' };
		case 'processing':
			return { label: 'Sending', detail: p.note || 'On its way to your bank.', tone: '' };
		case 'failed':
			return { label: 'Needs attention', detail: 'The transfer didn’t go through. We’ve been alerted and will send it again once it’s fixed.', tone: 'alert-critical' };
		case 'cancelled':
			return { label: 'No payout', detail: 'The buyer was refunded in full.', tone: '' };
		default:
			if (p.hold) return { label: 'On hold', detail: p.hold_text || 'On hold.', tone: 'alert-warning' };
			if (new Date(p.release_at) > new Date()) return { label: 'Scheduled', detail: `Due ${when(p.release_at)}`, tone: '' };
			return { label: 'Due', detail: p.note || 'Being prepared.', tone: '' };
	}
}

export const payoutAmount = (p: Payout) => (p.state === 'scheduled' ? p.entitlement_minor : (p.transfer_minor ?? 0));
