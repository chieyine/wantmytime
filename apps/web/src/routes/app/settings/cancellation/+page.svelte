<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { formatNaira } from '$lib/money';

	type Policy = { key: string; name: string; summary: string };
	let options = $state<Policy[]>([]);
	let chosen = $state('');
	let saved = $state('');
	let message = $state('');
	let busy = $state(false);
	let owed = $state<{ outstanding_minor: number; max_share_bps: number } | null>(null);

	onMount(async () => {
		try {
			const data = await api<{ policy: string; options: Policy[] }>('/api/v1/me/cancellation-policy');
			options = data.options;
			chosen = saved = data.policy;
			owed = await api('/api/v1/me/refund-recoveries');
		} catch (e) {
			message = e instanceof Error ? e.message : 'Your policy could not be loaded.';
		}
	});

	async function save() {
		busy = true;
		message = '';
		try {
			const p = await api<Policy>('/api/v1/me/cancellation-policy', { method: 'PUT', body: JSON.stringify({ policy: chosen }) });
			saved = p.key;
			message = `Saved. New bookings use the ${p.name.toLowerCase()} policy; existing bookings keep the policy they were made under.`;
		} catch (e) {
			message = e instanceof Error ? e.message : 'Your policy could not be saved.';
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>Cancellation policy — WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/app/settings">← Settings</a>
	<p class="eyebrow">Cancellation policy</p>
	<h1 class="page-heading">What buyers get back.</h1>
	<p class="page-intro">Choose how much a buyer is refunded if they cancel. Buyers see it before they pay. If you cancel, or don’t show up, the buyer is always refunded in full, and your share of that refund is taken from your next bookings.</p>
	{#if options.length}
		<form class="setup-form" onsubmit={(e) => { e.preventDefault(); save(); }}>
			<fieldset>
				<legend>Policy for new bookings</legend>
				<div class="policy-options">
					{#each options as option}
						<label class:chosen={chosen === option.key}><input type="radio" name="policy" value={option.key} bind:group={chosen} /> <span><strong>{option.name}</strong><small>{option.summary}</small></span></label>
					{/each}
				</div>
			</fieldset>
			<p class="form-note">Every policy gives a full refund to a buyer who cancels within an hour of booking, if the time is at least a day away.</p>
			<button class="button" type="submit" disabled={busy || chosen === saved}>Save policy</button>
		</form>
	{/if}
	{#if owed && owed.outstanding_minor > 0}
		<div class="notice notice-warning">You have {formatNaira(owed.outstanding_minor)} to repay from earlier refunds. Up to {owed.max_share_bps / 100}% of your share on each new booking goes towards it until it is cleared.</div>
	{/if}
	{#if message}<p class="notice notice-info" aria-live="polite">{message}</p>{/if}
</section>
