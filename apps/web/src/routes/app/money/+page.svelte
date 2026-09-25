<script lang="ts">
	import { formatMoney } from '$lib/money';
	import { payoutStatus, payoutAmount } from '$lib/payouts';

	let { data } = $props();
	let payouts = $derived(data.payouts?.payouts ?? []);
	let upcoming = $derived(data.payouts?.upcoming_minor ?? 0);
	let paid = $derived(data.payouts?.paid_minor ?? 0);
	let paused = $derived(data.payouts?.paused ?? false);
	let currency = $derived(data.payouts?.currency || 'NGN');
	const formatNaira = (minor: number) => formatMoney(minor, currency);
	let hasBank = $derived(data.hasBank);
	let message = $derived(data.loadError);
</script>

<svelte:head><title>Money — WantMyTime</title></svelte:head>
<section class="form-page app-page workspace-money">
	<a class="back-link" href="/app">← Overview</a>
	<p class="eyebrow">Money</p>
	<h1 class="page-heading">What you’ve made.</h1>
	<p class="page-intro">
		People pay before the call. We hold it until the buyer’s time to report a problem has passed, then send your share
		to your payout account. Most payouts land about three hours after the call.
	</p>
	{#if paused}<p class="notice notice-warning">
			Payouts are paused for a short while on our side. Nothing is lost; they go out as soon as the pause lifts.
		</p>{/if}
	{#if hasBank === false}<div class="notice notice-warning">
			Add your payout account so we know where to send your money. <a class="text-link" href="/app/settings/payouts"
				>Add it now ↗</a
			>
		</div>{/if}
	{#if message}<div class="notice notice-warning" role="alert">{message}</div>
	{:else}
		<div class="money-status-grid">
			<div>
				<span>ON THE WAY</span><strong>{formatNaira(upcoming)}</strong><small>Booked and paid for, not sent yet</small>
			</div>
			<div><span>PAID OUT</span><strong>{formatNaira(paid)}</strong><small>Already sent</small></div>
		</div>
		{#if payouts.length === 0}
			<div class="money-empty">
				<h2>NOTHING YET.</h2>
				<p>Your first payout shows up here the moment someone pays for your time.</p>
				<a class="button button-secondary" href="/app/share">Share your link ↗</a>
			</div>
		{:else}
			<div class="list-stack">
				{#each payouts as p (p.id)}
					{@const s = payoutStatus(p)}
					<article class="list-card">
						<div>
							<strong>{formatNaira(payoutAmount(p))} · {p.buyer_name}</strong>
							<p>{s.detail}</p>
							{#if p.recovery_minor > 0}<small>{formatNaira(p.recovery_minor)} kept to cover an earlier refund.</small
								>{/if}
							<small
								><a class="text-link" href={`/app/bookings/${encodeURIComponent(p.booking_id)}`}>View booking</a></small
							>
						</div>
						<span class={s.tone}>{s.label.toUpperCase()}</span>
					</article>
				{/each}
			</div>
		{/if}
		<p class="form-note">
			We keep 5% of each booking. Payout details are under <a class="text-link" href="/app/settings/payouts"
				>Settings → Payouts</a
			>.
		</p>
	{/if}
</section>
