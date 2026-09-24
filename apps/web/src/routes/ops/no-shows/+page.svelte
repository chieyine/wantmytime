<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';

	type Report = { id: string; booking_id: string; seller: string; buyer_name: string; absent_role: string; state: string; dispute_reason: string; resolves_at: string; created_at: string; starts_at: string };
	let reports = $state<Report[]>([]);
	let message = $state('');
	let busy = $state('');
	let reasons = $state<Record<string, string>>({});

	async function load() {
		try { reports = (await api<{ reports: Report[] }>('/api/v1/ops/no-shows')).reports; }
		catch (e) { message = e instanceof Error ? e.message : 'No-show reports could not be loaded.'; }
	}
	onMount(load);

	async function resolve(id: string, outcome: 'buyer_absent' | 'seller_absent' | 'both_attended') {
		busy = id;
		message = '';
		try {
			await api(`/api/v1/ops/no-shows/${encodeURIComponent(id)}/resolve`, { method: 'POST', body: JSON.stringify({ outcome, reason: reasons[id] ?? '' }) });
			message = 'Resolved. Both people have been emailed.';
			await load();
		} catch (e) { message = e instanceof Error ? e.message : 'The report could not be resolved.'; }
		finally { busy = ''; }
	}
	const when = (v: string) => new Date(v).toLocaleString();
</script>

<svelte:head><title>No-shows — Ops · WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/ops">← Operations</a>
	<p class="eyebrow">No-shows</p>
	<h1 class="page-heading">No-show reports.</h1>
	<p class="page-intro">An undisputed report stands after 48 hours. When the seller is absent the buyer is refunded in full; when the buyer is absent the booking stands. Disputed reports wait for a decision here.</p>
	{#if message}<p class="notice notice-info" aria-live="polite">{message}</p>{/if}
	{#if reports.length === 0}<p class="page-intro">No reports.</p>{/if}
	<div class="list-stack">
		{#each reports as r (r.id)}
			<article class="list-card">
				<div>
					<strong>{r.absent_role === 'seller' ? `@${r.seller} reported absent` : `${r.buyer_name} reported absent`}</strong>
					<p>Booking at {when(r.starts_at)} · <a href={`/ops/bookings/${encodeURIComponent(r.booking_id)}`}>booking ↗</a> · reported {when(r.created_at)}</p>
					{#if r.dispute_reason}<small>Their response: “{r.dispute_reason}”</small>{/if}
					{#if r.state === 'open'}<small>Stands on {when(r.resolves_at)} unless disputed.</small>{/if}
					{#if r.state === 'disputed' || r.state === 'open'}
						<div class="setup-form">
							<label>Evidence reviewed<input class="field" bind:value={reasons[r.id]} placeholder="What you checked" /></label>
							<div class="ops-nav">
								<button class="button" onclick={() => resolve(r.id, 'seller_absent')} disabled={busy === r.id || (reasons[r.id] ?? '').trim().length < 8}>Seller was absent (refund)</button>
								<button class="button" onclick={() => resolve(r.id, 'buyer_absent')} disabled={busy === r.id || (reasons[r.id] ?? '').trim().length < 8}>Buyer was absent</button>
								<button class="button button-secondary" onclick={() => resolve(r.id, 'both_attended')} disabled={busy === r.id || (reasons[r.id] ?? '').trim().length < 8}>Both attended</button>
							</div>
						</div>
					{/if}
				</div>
				<span class:alert-critical={r.state === 'disputed'}>{r.state.toUpperCase()}</span>
			</article>
		{/each}
	</div>
</section>
