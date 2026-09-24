<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { formatNaira } from '$lib/money';

	type Refund = { id: string; booking_id: string; seller: string; amount_minor: number; platform_share_minor: number; seller_share_minor: number; reason: string; state: string; provider_refund_id: string; last_error: string; note: string; attempts: number; created_at: string; processed_at: string | null };
	let refunds = $state<Refund[]>([]);
	let automatic = $state(false);
	let message = $state('');
	let busy = $state('');
	let reasons = $state<Record<string, string>>({});
	const reasonLabels: Record<string, string> = { buyer_cancelled: 'Buyer cancelled', seller_cancelled: 'Seller cancelled', seller_no_show: 'Seller no-show', operator: 'Operator refund', problem_upheld: 'Problem upheld' };
	const stateLabels: Record<string, string> = { pending_approval: 'Needs approval', queued: 'Queued', submitted: 'With Kora', processed: 'Refunded', failed: 'Failed', not_required: 'Nothing to refund' };

	async function load() {
		try {
			const data = await api<{ refunds: Refund[]; automatic_refunds: boolean }>('/api/v1/ops/refunds');
			refunds = data.refunds;
			automatic = data.automatic_refunds;
		} catch (e) {
			message = e instanceof Error ? e.message : 'Refunds could not be loaded.';
		}
	}
	onMount(load);

	async function act(id: string, action: 'approve' | 'retry' | 'record') {
		busy = id;
		message = '';
		try {
			await api(`/api/v1/ops/refunds/${encodeURIComponent(id)}/${action}`, { method: 'POST', body: JSON.stringify({ reason: reasons[id] ?? '' }) });
			message = action === 'record' ? 'Recorded as refunded. The buyer has been emailed.' : 'Refund queued for Kora.';
			await load();
		} catch (e) {
			message = e instanceof Error ? e.message : 'The refund could not be updated.';
		} finally {
			busy = '';
		}
	}
	const when = (v: string) => new Date(v).toLocaleString();
</script>

<svelte:head><title>Refunds — Ops · WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/ops">← Operations</a>
	<p class="eyebrow">Refunds</p>
	<h1 class="page-heading">Refunds.</h1>
	<p class="page-intro">Refunds come out of the platform’s Kora balance. If the seller has not been paid yet, their share comes out of the held payout; if they have, it is recovered from their next payouts. {automatic ? 'Automatic refunds are on: policy refunds go to Kora without approval.' : 'Automatic refunds are off: approve each refund, or refund in the Kora dashboard and record it here.'}</p>
	{#if message}<p class="notice notice-info" aria-live="polite">{message}</p>{/if}
	{#if refunds.length === 0}<p class="page-intro">No refunds yet.</p>{/if}
	<div class="list-stack">
		{#each refunds as r (r.id)}
			<article class="list-card">
				<div>
					<strong>{formatNaira(r.amount_minor)} · {reasonLabels[r.reason] ?? r.reason}</strong>
					<p>@{r.seller} · <a href={`/ops/bookings/${encodeURIComponent(r.booking_id)}`}>booking ↗</a> · seller share {formatNaira(r.seller_share_minor)} · requested {when(r.created_at)}{r.processed_at ? ` · refunded ${when(r.processed_at)}` : ''}</p>
					{#if r.last_error}<small>{r.last_error}</small>{/if}
					{#if r.note}<small>{r.note}</small>{/if}
					{#if r.state === 'pending_approval' || r.state === 'failed'}
						<div class="setup-form">
							<label>Evidence or reference<input class="field" bind:value={reasons[r.id]} placeholder="e.g. Seller cancelled; Kora refund RF-123" /></label>
							<div class="ops-nav">
								{#if r.state === 'pending_approval' && automatic}<button class="button" onclick={() => act(r.id, 'approve')} disabled={busy === r.id || (reasons[r.id] ?? '').trim().length < 8}>Approve and send</button>{/if}
								{#if r.state === 'failed' && !r.provider_refund_id}<button class="button" onclick={() => act(r.id, 'retry')} disabled={busy === r.id || (reasons[r.id] ?? '').trim().length < 8}>Retry</button>{/if}
								<button class="button button-secondary" onclick={() => act(r.id, 'record')} disabled={busy === r.id || (reasons[r.id] ?? '').trim().length < 8}>Record as refunded in Kora</button>
							</div>
						</div>
					{/if}
				</div>
				<span class:alert-critical={r.state === 'failed'}>{(stateLabels[r.state] ?? r.state).toUpperCase()}</span>
			</article>
		{/each}
	</div>
</section>
