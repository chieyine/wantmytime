<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	type Summary = {
		people: number;
		bookings: number;
		open_offers: number;
		overdue_meeting_links: number;
		open_cancellation_requests: number;
		product_events_7d: { event: string; count: number }[];
		payment_collection: string;
		settlement_reconciliation: string;
	};
	let summary = $state<Summary | null>(null);
	let message = $state('');
	onMount(async () => {
		try {
			summary = await api<Summary>('/api/v1/ops/overview');
		} catch (e) {
			message = e instanceof Error ? e.message : 'Operations summary is unavailable.';
		}
	});
</script>

<svelte:head><title>Operations — WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<p class="eyebrow">Operations</p>
	<h1 class="page-heading">A clear view of the work.</h1>
	{#if summary}<p class="page-intro">Live operational records from the service. Values refresh when you reload.</p>
		<div class="appointment-slip">
			<div><small>PEOPLE</small><strong>{summary.people}</strong></div>
			<div><small>BOOKINGS</small><strong>{summary.bookings}</strong></div>
			<div><small>OPEN OFFERS</small><strong>{summary.open_offers}</strong></div>
			<div><small>OVERDUE LINKS</small><strong>{summary.overdue_meeting_links}</strong></div>
			<div><small>CANCELLATION REQUESTS</small><strong>{summary.open_cancellation_requests}</strong></div>
		</div>
		<nav class="ops-nav">
			<a href="/ops/people">People</a><a href="/ops/bookings">Bookings</a><a href="/ops/cancellations"
				>Cancellation requests</a
			><a href="/ops/payouts">Payouts</a><a href="/ops/refunds">Refunds</a><a href="/ops/no-shows">No-shows</a><a
				href="/ops/reviews">Reviews</a
			><a href="/ops/offers">Offers</a><a href="/ops/meetings">Meeting delivery</a><a href="/ops/audit">Audit history</a
			><a href="/ops/payments">Payments</a><a href="/ops/settlements">Settlements</a><a href="/ops/provider-events"
				>Provider events</a
			><a href="/ops/provider-cases">Disputes and refunds</a><a href="/ops/exceptions">Payment exceptions</a><a
				href="/ops/growth">Growth</a
			><a href="/ops/marketing">Announcements</a><a href="/ops/system">System health</a><a href="/ops/settings"
				>Safeguards</a
			>
		</nav>
		<section class="readiness-panel">
			<h2>Product activity · last 7 days</h2>
			{#if summary.product_events_7d.length}<ul>
					{#each summary.product_events_7d as item (item.event)}<li>
							{item.event.replaceAll('_', ' ')} <strong>{item.count}</strong>
						</li>{/each}
				</ul>{:else}<p>No activity has been recorded yet.</p>{/if}
			<p class="form-note">
				Counts combine privacy-limited public-link interaction with server-recorded profile publication and
				provider-verified paid bookings. They exclude names, contact details and payment amounts.
			</p>
		</section>
		<div class="notice notice-warning">
			{summary.payment_collection === 'disabled'
				? 'Provider collection is disabled. Synthetic local transactions do not represent money.'
				: 'Provider checkout is enabled by configured environment gates.'}
		</div>
		<p class="form-note">
			Normalized CSV settlement import is available to authorized operators; provider export compatibility and sandbox
			reconciliation still require approval.
		</p>{:else}<p class="page-intro">{message || 'Loading operational overview…'}</p>
		{#if message}<div class="setup-form">
				<a class="button" href="/ops/access">Verify operations access</a><a
					class="text-link"
					href="/login?next=%2Fops%2Faccess">Sign in to your verified account first</a
				>
			</div>{/if}{/if}
</section>
