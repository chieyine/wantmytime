<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	type EventCount = { event: string; count: number };
	type Cohort = { month: string; attributed_relationships: number; observed_30_days: number };
	type Growth = {
		events_7d: EventCount[];
		verified_buyer_to_seller_relationships: number;
		observed_30_days: number;
		cohorts: Cohort[];
		attribution_rule: string;
		limitations: string[];
	};
	type Step = { key: string; label: string; count: number; basis: string };
	type Funnels = {
		days: number;
		buyer: Step[];
		seller: Step[];
		median_hours_to_first_paid: number | null;
		includes_simulated_payments: boolean;
		notes: string[];
	};
	let growth = $state<Growth | null>(null);
	let funnels = $state<Funnels | null>(null);
	let days = $state(30);
	let message = $state('');
	let funnelMessage = $state('');
	async function loadFunnels(d: number) {
		days = d;
		funnelMessage = '';
		try {
			funnels = await api<Funnels>(`/api/v1/ops/funnels?days=${d}`);
		} catch (error) {
			funnelMessage = error instanceof Error ? error.message : 'Funnels could not be loaded.';
		}
	}
	onMount(async () => {
		try {
			growth = await api<Growth>('/api/v1/ops/growth');
		} catch (error) {
			message = error instanceof Error ? error.message : 'Growth activity could not be loaded.';
		}
		void loadFunnels(30);
	});
	const pct = (n: number, d: number) => (d > 0 ? `${Math.round((n / d) * 100)}%` : '—');
	function widths(steps: Step[]) {
		const top = Math.max(1, ...steps.map((s) => s.count));
		return steps.map((s) => Math.max(s.count > 0 ? 1.5 : 0, (s.count / top) * 100));
	}
</script>

<svelte:head><title>Growth — Ops · WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/ops">← Operations</a>
	<p class="eyebrow">Growth measurement</p>
	<h1 class="page-heading">Count what happened.</h1>
	<div class="funnel-range" role="group" aria-label="Period">
		{#each [7, 30, 90] as d (d)}<button
				type="button"
				class:selected={days === d}
				aria-pressed={days === d}
				onclick={() => loadFunnels(d)}>Last {d} days</button
			>{/each}
	</div>
	{#if funnels}
		{#each [{ title: 'Buyers: from page view to session', steps: funnels.buyer, of: 'of views' }, { title: 'Sellers: from claimed link to paid booking', steps: funnels.seller, of: 'of claimed' }] as f (f.title)}
			{@const w = widths(f.steps)}
			<section class="readiness-panel funnel">
				<h2>{f.title}</h2>
				<table class="funnel-table">
					<thead
						><tr
							><th scope="col">Step</th><th scope="col"><span class="sr-only">Share of first step</span></th><th
								scope="col">Count</th
							><th scope="col">From previous</th></tr
						></thead
					>
					<tbody>
						{#each f.steps as step, i (step.key)}
							<tr title={`${step.label}: ${step.count} ${step.basis}, ${pct(step.count, f.steps[0].count)} ${f.of}`}>
								<th scope="row">{step.label}<small>{step.basis}</small></th>
								<td class="funnel-bar-cell" aria-hidden="true"
									><span class="funnel-bar" style:width={`${w[i]}%`}></span></td
								>
								<td class="num">{step.count.toLocaleString()}</td>
								<td class="num">{i === 0 ? '' : pct(step.count, f.steps[i - 1].count)}</td>
							</tr>
						{/each}
					</tbody>
				</table>
				<p class="form-note">
					{pct(f.steps[f.steps.length - 1].count, f.steps[0].count)}
					{f.of} reached the last step in the last {funnels.days} days.
				</p>
			</section>
		{/each}
		<p class="form-note">
			Median time from claiming a link to the first paid booking: {funnels.median_hours_to_first_paid === null
				? 'no paid bookings yet'
				: funnels.median_hours_to_first_paid < 48
					? `${Math.round(funnels.median_hours_to_first_paid)} hours`
					: `${Math.round(funnels.median_hours_to_first_paid / 24)} days`}.
		</p>
		{#if funnels.includes_simulated_payments}<p class="notice notice-info">
				This is not production, so simulated payments count as paid.
			</p>{/if}
		{#each funnels.notes as note (note)}<p class="form-note">{note}</p>{/each}
	{:else if funnelMessage}<div class="notice notice-warning" role="status">{funnelMessage}</div>{/if}
	{#if growth}
		<p class="page-intro">
			First-party activity recorded in the last seven days. Event counts describe actions, not people, paid conversion,
			or virality.
		</p>
		{#if growth.events_7d.length}
			<div class="appointment-slip">
				{#each growth.events_7d as item (item.event)}<div>
						<small>{item.event.replaceAll('_', ' ').toUpperCase()}</small><strong>{item.count}</strong>
					</div>{/each}
			</div>
		{:else}<div class="notice notice-info">No eligible activity was recorded in the last seven days.</div>{/if}
		<section class="readiness-panel">
			<h2>Verified buyer to seller activity</h2>
			<div class="appointment-slip">
				<div>
					<small>ATTRIBUTED RELATIONSHIPS</small><strong>{growth.verified_buyer_to_seller_relationships}</strong>
				</div>
				<div><small>OBSERVED FOR 30+ DAYS</small><strong>{growth.observed_30_days}</strong></div>
			</div>
			<p>{growth.attribution_rule}</p>
			{#if growth.cohorts.length}<div class="list-stack">
					{#each growth.cohorts as cohort (cohort.month)}<article class="list-card">
							<div>
								<strong>{cohort.month}</strong>
								<p>{cohort.attributed_relationships} attributed relationships</p>
								<small>{cohort.observed_30_days} have 30 or more days of observation</small>
							</div>
						</article>{/each}
				</div>{:else}<p>No qualifying live-payment relationships have been recorded.</p>{/if}
			{#each growth.limitations as limitation (limitation)}<p class="form-note">{limitation}</p>{/each}
		</section>
		<section class="readiness-panel">
			<h2>Measurement boundaries</h2>
			<p>
				Public-link interactions and server-recorded profile publication or verified paid-booking events are aggregated
				without names, contact details, or amounts. Clicks and event counts are not conversions. No public social graph
				is produced.
			</p>
		</section>
	{:else}
		<div class="notice notice-warning" role="status">{message || 'Loading real event and attribution records…'}</div>
		{#if message}<a class="button button-secondary" href="/ops/access">Verify operations access ↗</a>{/if}
	{/if}
</section>
