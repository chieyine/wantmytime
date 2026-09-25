<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { formatMoney } from '$lib/money';

	type Payout = {
		currency: string;
		id: string;
		booking_id: string;
		seller: string;
		buyer_name: string;
		state: string;
		entitlement_minor: number;
		amount_minor: number | null;
		transfer_minor?: number;
		recovery_minor: number;
		fee_minor: number;
		release_at: string;
		paid_at: string | null;
		bank_name: string;
		account_last4: string;
		hold: string;
		hold_text?: string;
		last_error: string;
		attempts: number;
		reference: string;
	};
	let payouts = $state<Payout[]>([]);
	let paused = $state(false);
	let onlyAttention = $state(true);
	let message = $state('');
	let busy = $state('');
	let reasons = $state<Record<string, string>>({});
	const stateLabels: Record<string, string> = {
		scheduled: 'Scheduled',
		processing: 'Sending',
		paid: 'Paid',
		failed: 'Failed',
		cancelled: 'Nothing to pay'
	};

	async function load() {
		try {
			const data = await api<{ payouts: Payout[]; paused: boolean }>(
				`/api/v1/ops/payouts${onlyAttention ? '?state=attention' : ''}`
			);
			payouts = data.payouts;
			paused = data.paused;
		} catch (e) {
			message = e instanceof Error ? e.message : 'Payouts could not be loaded.';
		}
	}
	onMount(load);

	async function retry(id: string) {
		busy = id;
		message = '';
		try {
			await api(`/api/v1/ops/payouts/${encodeURIComponent(id)}/retry`, {
				method: 'POST',
				body: JSON.stringify({ reason: reasons[id] ?? '' })
			});
			message = 'Payout queued again under a new reference.';
			await load();
		} catch (e) {
			message = e instanceof Error ? e.message : 'The payout could not be retried.';
		} finally {
			busy = '';
		}
	}
	const when = (v: string) => new Date(v).toLocaleString();
</script>

<svelte:head><title>Payouts — Ops · WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/ops">← Operations</a>
	<p class="eyebrow">Payouts</p>
	<h1 class="page-heading">Seller payouts.</h1>
	<p class="page-intro">
		Each paid booking’s seller share is held, then transferred from the platform balance after the buyer’s problem
		window closes. A payout waits (it does not fail) while the balance has not settled or the seller has no bank
		account. Failed and reversed transfers need a retry once the cause is fixed.
	</p>
	{#if paused}<p class="notice notice-warning">PAYOUTS_PAUSED is on: no transfers are being sent.</p>{/if}
	<label class="form-note"
		><input type="checkbox" bind:checked={onlyAttention} onchange={load} /> Only payouts that need attention</label
	>
	{#if message}<p class="notice notice-info" aria-live="polite">{message}</p>{/if}
	{#if payouts.length === 0}<p class="page-intro">
			{onlyAttention ? 'Nothing needs attention.' : 'No payouts yet.'}
		</p>{/if}
	<div class="list-stack">
		{#each payouts as p (p.id)}
			<article class="list-card">
				<div>
					<strong
						>{formatMoney(p.amount_minor === null ? p.entitlement_minor : (p.transfer_minor ?? 0), p.currency)} to @{p.seller}{p.account_last4
							? ` · ${p.bank_name} ••${p.account_last4}`
							: ''}</strong
					>
					<p>
						{p.buyer_name} · <a href={`/ops/bookings/${encodeURIComponent(p.booking_id)}`}>booking ↗</a> · due {when(
							p.release_at
						)}{p.paid_at ? ` · paid ${when(p.paid_at)}` : ''}{p.recovery_minor > 0
							? ` · ${formatMoney(p.recovery_minor, p.currency)} kept for earlier refunds`
							: ''}{p.fee_minor > 0 ? ` · transfer fee ${formatMoney(p.fee_minor, p.currency)}` : ''}
					</p>
					{#if p.hold_text}<small>{p.hold_text}</small>{/if}
					{#if p.last_error}<small>{p.last_error}</small>{/if}
					<small>Ref {p.reference} · {p.attempts} attempt{p.attempts === 1 ? '' : 's'}</small>
					{#if p.state === 'failed'}
						<div class="setup-form">
							<label
								>What was checked<input
									class="field"
									bind:value={reasons[p.id]}
									placeholder="e.g. Seller updated their bank account"
								/></label
							>
							<button
								class="button"
								onclick={() => retry(p.id)}
								disabled={busy === p.id || (reasons[p.id] ?? '').trim().length < 8}>Retry payout</button
							>
						</div>
					{/if}
				</div>
				<span class:alert-critical={p.state === 'failed'}>{(stateLabels[p.state] ?? p.state).toUpperCase()}</span>
			</article>
		{/each}
	</div>
</section>
