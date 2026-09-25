<script lang="ts">
	import PageMeta from '$lib/components/PageMeta.svelte';
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { currencySymbol, formatMoney, parseMoneyToMinor } from '$lib/money';
	type Market = { country: string; name: string; currency: string };
	let markets = $state<Market[]>([{ country: 'NG', name: 'Nigeria', currency: 'NGN' }]);
	let currency = $state('NGN');
	let gross = $state('10000');
	let amount = $derived(Number(parseMoneyToMinor(gross) ?? 0n));
	let fee = $derived(Math.floor((amount * 500) / 10000));
	onMount(async () => {
		try {
			const list = (await api<{ markets: Market[] }>('/api/v1/markets')).markets;
			if (list.length) markets = list;
		} catch {
			// The calculator works in naira without the list.
		}
	});
</script>
<PageMeta title="Pricing — WantMyTime" description="Free to set up. WantMyTime keeps 5% of each paid booking, and nothing else." />
<section class="article-page">
	<p class="eyebrow">Pricing</p>
	<h1 class="page-heading">5%. That’s it.</h1>
	<p class="page-intro">No subscription, no setup fee. When someone pays for your time, we keep 5% and send you the rest. If nobody books, you pay nothing.</p>
	<div class="calculator">
		{#if markets.length > 1}<label>Currency<select class="field" bind:value={currency}>{#each markets as m (m.currency)}<option value={m.currency}>{m.name} ({m.currency})</option>{/each}</select></label>{/if}
		<label>They pay you<div class="money-input"><span>{currencySymbol(currency).trim()}</span><input bind:value={gross} inputmode="decimal" aria-label={`Booking price in ${currency}`} /></div></label>
		<div><span>We keep (5%)</span><strong>{formatMoney(fee, currency)}</strong></div>
		<div><span>You get</span><strong>{formatMoney(amount - fee, currency)}</strong></div>
		<small>Payment charges (bank transfer, pay with bank or mobile money) are paid by the person booking, not taken from your share.</small>
	</div>
	<h2>When the money arrives</h2>
	<p>The person booking pays before the call. We hold it while the call happens and for two hours after, in case something went wrong. Then we send your share to your bank account or mobile money wallet. Most people see it about three hours after the call ends. A pay-with-bank or mobile money payment can take one extra working day to reach us before it can be paid out.</p>
	<h2>Refunds</h2>
	<p>You choose your cancellation policy, and buyers see it before they pay. If they cancel, they get back what your policy allows, but not the payment fee. If you cancel or don’t show up, they get everything back, payment fee included, and you aren’t paid for that booking.</p>
	<h2>Where it works</h2>
	<p>People can book you from anywhere and see your times in their own timezone. They pay in your currency, by bank transfer or pay with bank in Nigeria and by mobile money in Ghana and Kenya. We don’t take cards. {markets.length > 1 ? `Sellers can be paid in ${markets.map((m) => m.name).join(', ')}.` : 'Sellers are paid in Nigeria for now, with more countries coming.'}</p>
</section>
