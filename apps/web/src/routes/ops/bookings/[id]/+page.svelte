<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { formatMoney } from '$lib/money';
	let { params } = $props();
	type Payout = { id: string; state: string; entitlement_minor: number; amount_minor: number | null; transfer_minor?: number; release_at: string; paid_at: string | null; hold_text?: string; last_error: string };
	type Booking = { id: string; seller: string; buyer_name: string; guest_email: string; duration_minutes: number; starts_at: string; gross_minor: number; currency: string; state: string; payment_state: string; created_at: string; meeting_deadline: string; meeting_ready_at: string | null; issue_reason: string | null; issue_resolved: boolean; issue_reported_by: string; issue_resolution: string; issue_seller_response?: string; issue_seller_note?: string; issue_disputed?: boolean; payout: Payout | null };
	let booking = $state<Booking | null>(null);
	let message = $state('');
	let resolution = $state('');
	let refundNaira = $state('');
	let busy = $state(false);
	const refundMinor = $derived(Math.round((Number(refundNaira.replace(/[,\s]/g, '')) || 0) * 100));

	async function load() {
		try {
			booking = await api<Booking>(`/api/v1/ops/bookings/${encodeURIComponent(params.id)}`);
		} catch (e) {
			message = e instanceof Error ? e.message : 'Booking could not be loaded.';
		}
	}
	onMount(load);

	async function resolve() {
		busy = true;
		message = '';
		try {
			await api(`/api/v1/ops/bookings/${encodeURIComponent(params.id)}/resolve-issue`, { method: 'POST', body: JSON.stringify({ resolution, refund_minor: refundMinor }) });
			message = refundMinor > 0 ? 'Resolved. The refund has been created and the rest of the payout is released.' : 'Resolved. The payout is released.';
			resolution = refundNaira = '';
			await load();
		} catch (e) {
			message = e instanceof Error ? e.message : 'The problem could not be resolved.';
		} finally {
			busy = false;
		}
	}
</script>
<svelte:head><title>Booking detail — Ops · WantMyTime</title></svelte:head>
<section class="form-page app-page"><a class="back-link" href="/ops/bookings">← Bookings</a><p class="eyebrow">Booking record</p>
{#if booking}<h1 class="page-heading">{booking.buyer_name} with @{booking.seller}</h1>
{#if message}<p class="notice notice-info" aria-live="polite">{message}</p>{/if}
<div class="list-stack">
<article class="list-card"><div><strong>Appointment</strong><p>{booking.duration_minutes} minutes · {new Date(booking.starts_at).toLocaleString()}</p><small>Created {new Date(booking.created_at).toLocaleString()}</small></div></article>
<article class="list-card"><div><strong>Payment record</strong><p>{formatMoney(booking.gross_minor, booking.currency)} · {booking.currency}</p><small>Booking: {booking.state} · Payment: {booking.payment_state}</small></div></article>
{#if booking.payout}<article class="list-card"><div><strong>Seller payout</strong><p>{formatMoney(booking.payout.amount_minor === null ? booking.payout.entitlement_minor : (booking.payout.transfer_minor ?? 0), booking.currency)} · {booking.payout.state}{booking.payout.paid_at ? ` ${new Date(booking.payout.paid_at).toLocaleString()}` : ` · due ${new Date(booking.payout.release_at).toLocaleString()}`}</p>{#if booking.payout.hold_text}<small>{booking.payout.hold_text}</small>{/if}{#if booking.payout.last_error}<small>{booking.payout.last_error}</small>{/if}<small><a href="/ops/payouts">All payouts ↗</a></small></div></article>{/if}
<article class="list-card"><div><strong>Conversation delivery</strong><p>{booking.meeting_ready_at ? `Link added ${new Date(booking.meeting_ready_at).toLocaleString()}` : 'Meeting link not yet added'}</p><small>Link deadline {new Date(booking.meeting_deadline).toLocaleString()}</small></div></article>
<article class="list-card"><div><strong>Reported problem{booking.issue_reported_by ? ` (by the ${booking.issue_reported_by})` : ''}</strong><p>{booking.issue_reason || 'No problem reported'}</p>{#if booking.issue_seller_note}<p><strong>Seller’s side:</strong> {booking.issue_seller_note}</p>{/if}<small>{booking.issue_resolved ? `Resolved${booking.issue_resolution ? `: ${booking.issue_resolution}` : ''}` : booking.issue_disputed ? 'Disputed by the seller: your decision' : booking.issue_reason && booking.issue_reported_by === 'buyer' ? 'Waiting for the seller to answer (settles by itself)' : booking.issue_reason ? 'Open' : '—'}</small>
{#if booking.issue_reason && !booking.issue_resolved}
	<form class="setup-form" onsubmit={(e) => { e.preventDefault(); resolve(); }}>
		<label>Outcome (sent to both people)<textarea class="field" rows="3" maxlength="500" bind:value={resolution} placeholder="What was reviewed and what was decided"></textarea></label>
		{#if booking.payment_state === 'paid'}<label>Refund to the buyer, in {booking.currency} (optional)<input class="field" inputmode="decimal" bind:value={refundNaira} placeholder="0" /></label>
		<p class="form-note">{refundMinor > 0 ? `${formatMoney(refundMinor, booking.currency)} is refunded; the seller’s share of it comes out of their payout, and the rest is released.` : 'No refund: the full payout is released.'}</p>{/if}
		<button class="button" type="submit" disabled={busy || resolution.trim().length < 8 || refundMinor > booking.gross_minor}>Resolve problem</button>
	</form>
{/if}
</div></article>
<article class="list-card"><div><strong>Buyer contact</strong><p>{booking.guest_email}</p><small>Private operational record</small></div></article>
</div>{:else if message}<div class="notice notice-warning" role="status">{message}</div>{:else}<p class="page-intro">Loading booking…</p>{/if}</section>
