<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { formatMoney } from '$lib/money';
	let { params } = $props();
	type Item = {
		id: string;
		booking_id: string;
		seller: string;
		starts_at: string;
		currency: string;
		gross_minor: number;
		deduction_minor: number;
		seller_entitlement_minor: number;
		route: string;
		state: string;
		provider_reference: string | null;
		settled_at: string | null;
	};
	let item = $state<Item | null>(null);
	let message = $state('');
	let loading = $state(true);
	const formatNaira = (minor: number) => formatMoney(minor, item?.currency || 'NGN');
	onMount(async () => {
		try {
			item = await api<Item>(`/api/v1/me/settlements/${encodeURIComponent(params.id)}`);
		} catch (error) {
			message = error instanceof Error ? error.message : 'This settlement record is unavailable.';
		} finally {
			loading = false;
		}
	});
	const dateLabel = (value: string) =>
		new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value));
</script>

<svelte:head><title>Settlement detail — WantMyTime</title></svelte:head>
<section class="form-page app-page workspace-money-detail">
	<a class="back-link" href="/app/money">← Money</a>
	<p class="eyebrow">Money / settlement record</p>
	{#if loading}<p class="page-intro">Loading your private record…</p>
	{:else if !item}<h1 class="page-heading">RECORD UNAVAILABLE.</h1>
		<p class="page-intro">{message}</p>
	{:else}<div class="money-detail-hero">
			<span class="record-status" class:urgent={!item.settled_at}
				>{item.settled_at ? 'PROVIDER-CONFIRMED SETTLED' : 'AWAITING SETTLEMENT EVIDENCE'}</span
			>
			<h1>{formatNaira(item.seller_entitlement_minor)}</h1>
			<p>Your allocation from the booking on {dateLabel(item.starts_at)}.</p>
		</div>
		<div class="money-detail-grid">
			<section>
				<p class="workspace-kicker">01 / ALLOCATION</p>
				<dl>
					<div>
						<dt>Booking amount</dt>
						<dd>{formatNaira(item.gross_minor)}</dd>
					</div>
					<div>
						<dt>Deduction</dt>
						<dd>{formatNaira(item.deduction_minor)}</dd>
					</div>
					<div>
						<dt>Your allocated share</dt>
						<dd>{formatNaira(item.seller_entitlement_minor)}</dd>
					</div>
					<div>
						<dt>Settlement route</dt>
						<dd>{item.route.replaceAll('_', ' ')}</dd>
					</div>
				</dl>
			</section>
			<section>
				<p class="workspace-kicker">02 / PROVIDER EVIDENCE</p>
				<dl>
					<div>
						<dt>Settlement status</dt>
						<dd>{item.settled_at ? 'Matched and confirmed' : item.state.replaceAll('_', ' ')}</dd>
					</div>
					<div>
						<dt>Provider reference</dt>
						<dd>{item.provider_reference || 'Not reported'}</dd>
					</div>
					<div>
						<dt>Confirmed at</dt>
						<dd>{item.settled_at ? dateLabel(item.settled_at) : 'No matched confirmation recorded'}</dd>
					</div>
					<div>
						<dt>Booking ID</dt>
						<dd>{item.booking_id}</dd>
					</div>
				</dl>
			</section>
		</div>
		<p class="money-detail-note">
			An allocation records your share of a verified payment. Only matched provider evidence confirms that settlement
			occurred.
		</p>
	{/if}
</section>
