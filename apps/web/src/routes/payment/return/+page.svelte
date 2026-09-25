<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api';
	let quoteId = $state('');
	let reference = $state('');
	let paymentState = $state<'checking' | 'paid' | 'pending' | 'review' | 'error'>('checking');
	let message = $state('Checking your payment…');
	let busy = $state(false);
	let tries = 0;
	let timer: ReturnType<typeof setTimeout> | undefined;

	onMount(() => {
		const query = new URLSearchParams(window.location.search);
		quoteId = query.get('quote_id') ?? '';
		reference = query.get('reference') ?? query.get('trxref') ?? '';
		void verify();
	});
	onDestroy(() => clearTimeout(timer));

	// Pay-with-bank and mobile money payments can take a little while to settle, so a
	// pending answer is checked again automatically for about two minutes.
	async function verify() {
		clearTimeout(timer);
		if (!quoteId || !reference) {
			paymentState = 'error';
			message = 'This page is missing its payment reference. Open your booking from the confirmation email, or find it with your email address.';
			return;
		}
		busy = true;
		try {
			const result = await api<{ state: string; booking_id: string | null }>(`/api/v1/quotes/${encodeURIComponent(quoteId)}/verify-payment`, { method: 'POST', body: JSON.stringify({ reference }) });
			if (result.booking_id) {
				paymentState = 'paid';
				message = 'Payment confirmed. Taking you to your booking…';
				window.location.replace(`/booking/${encodeURIComponent(result.booking_id)}`);
			} else if (result.state === 'review') {
				paymentState = 'review';
				message = 'Your payment went through, but the time couldn’t be booked (it may have been taken while you paid). Don’t pay again: the full amount is being returned to you, and we’ve emailed you the details.';
			} else if (result.state === 'failed') {
				paymentState = 'error';
				message = 'The payment didn’t go through, and no money was taken. Your time may still be held: go back and try again.';
			} else {
				paymentState = 'pending';
				message = 'Your payment is still being confirmed. This page checks again by itself.';
				if (tries++ < 24) timer = setTimeout(verify, 5000);
				else message = 'Your payment hasn’t been confirmed yet. If money left your account, it will either confirm your booking shortly or be returned. We’ll email you either way.';
			}
		} catch (error) {
			paymentState = 'error';
			message = error instanceof Error ? error.message : 'Your payment could not be checked. You can safely try again.';
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>Checking payment — WantMyTime</title><meta name="robots" content="noindex,nofollow" /></svelte:head>
<section class="form-page">
	<p class="eyebrow">Payment status</p>
	<h1 class="page-heading">{paymentState === 'paid' ? 'Your time is confirmed.' : paymentState === 'review' ? 'We’re returning your payment.' : paymentState === 'error' ? 'Something needs your attention.' : 'Checking your payment.'}</h1>
	<p class="page-intro" aria-live="polite">{message}</p>
	{#if paymentState === 'pending' || paymentState === 'error'}<button class="button" onclick={() => { tries = 0; void verify(); }} disabled={busy || !quoteId || !reference}>{busy ? 'Checking…' : 'Check again'}</button>{/if}
	{#if paymentState === 'error' && quoteId}<a class="button button-secondary" href={`/checkout/${encodeURIComponent(quoteId)}`}>Back to payment</a>{/if}
	{#if paymentState === 'error'}<p class="form-note">Paid on another device or lost this page? <a class="text-link" href={`/access?next=${encodeURIComponent(`/payment/return?quote_id=${quoteId}&reference=${reference}`)}`}>Find your booking with your email</a>.</p>{/if}
</section>
