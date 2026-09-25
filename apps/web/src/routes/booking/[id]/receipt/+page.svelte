<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api } from '$lib/api';
	import { formatMoney, methodLabels } from '$lib/money';
	import { recordProductEvent } from '$lib/analytics';
	let { params } = $props();
	type Receipt = { id: string; buyer_name: string; seller_name: string; seller_handle: string; viewer_role: 'buyer' | 'seller'; has_seller_profile: boolean; starts_at: string; duration_minutes: number; currency: string; gross_minor: number; fee_minor?: number; total_minor?: number; deduction_minor?: number; seller_entitlement_minor?: number; refunded_minor?: number; payment_method?: string; payment_state: string; booking_state: string; paid_at: string; provider_reference: string };
	let receipt = $state<Receipt | null>(null);
	let message = $state('');
	let loading = $state(true);
	const money = (minor: number | undefined) => formatMoney(minor ?? 0, receipt?.currency || 'NGN');
	onMount(async () => {
		try {
			receipt = await api<Receipt>(`/api/v1/bookings/${encodeURIComponent(params.id)}/receipt`);
		} catch (e) {
			message = e instanceof Error ? e.message : 'A receipt is not available.';
		} finally {
			loading = false;
		}
	});
</script>

<svelte:head><title>Payment receipt — WantMyTime</title><meta name="robots" content="noindex,nofollow" /></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href={`/booking/${encodeURIComponent(params.id)}`}>← Booking</a>
	<p class="eyebrow">Payment receipt</p>
	{#if loading}
		<p class="page-intro">Loading your receipt…</p>
	{:else if !receipt}
		<h1 class="page-heading">Receipt unavailable.</h1>
		<p class="page-intro">{message}</p>
	{:else}
		<h1 class="page-heading">Payment received.</h1>
		<div class="appointment-slip">
			<div><small>PAID BY</small><strong>{receipt.buyer_name}</strong></div>
			<div><small>PAID TO</small><strong>{receipt.seller_name}</strong></div>
			<div><small>PRICE</small><strong>{money(receipt.gross_minor)}</strong></div>
			{#if receipt.viewer_role === 'buyer'}
				{#if (receipt.fee_minor ?? 0) > 0}<div><small>PAYMENT FEE</small><strong>{money(receipt.fee_minor)}</strong></div>{/if}
				<div><small>TOTAL PAID</small><strong>{money(receipt.total_minor ?? receipt.gross_minor)}</strong></div>
			{:else}
				<div><small>WANTMYTIME FEE</small><strong>{money(receipt.deduction_minor)}</strong></div>
				<div><small>YOUR SHARE</small><strong>{money(receipt.seller_entitlement_minor)}</strong></div>
			{/if}
			{#if (receipt.refunded_minor ?? 0) > 0}<div><small>REFUNDED</small><strong>{money(receipt.refunded_minor)}</strong></div>{/if}
			<div><small>DATE</small><strong>{new Date(receipt.paid_at).toLocaleString()}</strong></div>
			<div><small>BOOKING</small><strong>{new Date(receipt.starts_at).toLocaleString()} · {receipt.duration_minutes} minutes</strong></div>
			{#if receipt.payment_method}<div><small>PAID WITH</small><strong>{methodLabels[receipt.payment_method] ?? receipt.payment_method}</strong></div>{/if}
			<div><small>REFERENCE</small><strong>{receipt.provider_reference || '—'}</strong></div>
		</div>
		<p class="form-note">Only the two people on this booking can see this receipt.</p>
		{#if receipt.viewer_role === 'buyer' && !receipt.has_seller_profile}
			<section class="readiness-panel">
				<h2>Your time can have a link, too.</h2>
				<p>Share your own link when someone wants to talk with you.</p>
				<a class="button" href="/claim" onclick={async (event) => { event.preventDefault(); await recordProductEvent('seller_cta_clicked', receipt!.seller_handle, { booking_id: receipt!.id }); await goto('/claim'); }}>Create your link ↗</a>
			</section>
		{/if}
		<button class="button button-secondary" onclick={() => window.print()}>Print receipt</button>
	{/if}
</section>
