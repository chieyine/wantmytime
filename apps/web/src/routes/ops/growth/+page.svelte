<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	type EventCount = { event: string; count: number };
	type Cohort = { month: string; attributed_relationships: number; observed_30_days: number };
	type Growth = { events_7d: EventCount[]; verified_buyer_to_seller_relationships: number; observed_30_days: number; cohorts: Cohort[]; attribution_rule: string; limitations: string[] };
	let growth = $state<Growth | null>(null);
	let message = $state('');
	onMount(async () => {
		try { growth = await api<Growth>('/api/v1/ops/growth'); }
		catch (error) { message = error instanceof Error ? error.message : 'Growth activity could not be loaded.'; }
	});
</script>

<svelte:head><title>Growth — Ops · WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/ops">← Operations</a>
	<p class="eyebrow">Growth measurement</p>
	<h1 class="page-heading">Count what happened.</h1>
	{#if growth}
		<p class="page-intro">First-party activity recorded in the last seven days. Event counts describe actions, not people, paid conversion, or virality.</p>
		{#if growth.events_7d.length}
			<div class="appointment-slip">{#each growth.events_7d as item}<div><small>{item.event.replaceAll('_', ' ').toUpperCase()}</small><strong>{item.count}</strong></div>{/each}</div>
		{:else}<div class="notice notice-info">No eligible activity was recorded in the last seven days.</div>{/if}
		<section class="readiness-panel">
			<h2>Verified buyer to seller activity</h2>
			<div class="appointment-slip"><div><small>ATTRIBUTED RELATIONSHIPS</small><strong>{growth.verified_buyer_to_seller_relationships}</strong></div><div><small>OBSERVED FOR 30+ DAYS</small><strong>{growth.observed_30_days}</strong></div></div>
			<p>{growth.attribution_rule}</p>
			{#if growth.cohorts.length}<div class="list-stack">{#each growth.cohorts as cohort}<article class="list-card"><div><strong>{cohort.month}</strong><p>{cohort.attributed_relationships} attributed relationships</p><small>{cohort.observed_30_days} have 30 or more days of observation</small></div></article>{/each}</div>{:else}<p>No qualifying live-payment relationships have been recorded.</p>{/if}
			{#each growth.limitations as limitation}<p class="form-note">{limitation}</p>{/each}
		</section>
		<section class="readiness-panel"><h2>Measurement boundaries</h2><p>Public-link interactions and server-recorded profile publication or verified paid-booking events are aggregated without names, contact details, or amounts. Clicks and event counts are not conversions. No public social graph is produced.</p></section>
	{:else}
		<div class="notice notice-warning" role="status">{message || 'Loading real event and attribution records…'}</div>
		{#if message}<a class="button button-secondary" href="/ops/access">Verify operations access ↗</a>{/if}
	{/if}
</section>
