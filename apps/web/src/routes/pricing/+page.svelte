<script lang="ts">
	import { formatNaira, parseNairaToMinor } from '$lib/money';
	let gross = $state('10000');
	let amount = $derived(Number(parseNairaToMinor(gross) ?? 0n));
	let fee = $derived(Math.floor((amount * 500) / 10000));
</script>
<svelte:head><title>Pricing — WantMyTime</title><meta name="description" content="Free to set up. WantMyTime keeps 5% of each paid booking, and nothing else." /></svelte:head>
<section class="article-page">
	<p class="eyebrow">Pricing</p>
	<h1 class="page-heading">5%. That’s it.</h1>
	<p class="page-intro">No subscription, no setup fee. When someone pays for your time, we keep 5% and send you the rest. If nobody books, you pay nothing.</p>
	<div class="calculator">
		<label>They pay you<div class="money-input"><span>₦</span><input bind:value={gross} inputmode="decimal" aria-label="Booking price in naira" /></div></label>
		<div><span>We keep (5%)</span><strong>{formatNaira(fee)}</strong></div>
		<div><span>You get</span><strong>{formatNaira(amount - fee)}</strong></div>
		<small>Bank transfer charges are paid by the person booking, not taken from your share.</small>
	</div>
	<h2>When the money arrives</h2>
	<p>The person booking pays before the call. We hold it while the call happens and for two hours after, in case something went wrong. Then we send your share to your bank. Most people see it about three hours after the call ends.</p>
	<h2>Refunds</h2>
	<p>You choose your cancellation policy, and buyers see it before they pay. If they cancel, they get back what your policy allows, but not the transfer fee. If you cancel or don’t show up, they get everything back, transfer fee included, and you aren’t paid for that booking.</p>
</section>
