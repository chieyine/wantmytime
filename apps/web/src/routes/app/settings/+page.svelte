<script lang="ts">
	import { untrack } from 'svelte';
	import PushToggle from '$lib/components/PushToggle.svelte';
	import { marketingWording, saveMarketingChoice } from '$lib/marketing';
	let { data } = $props();
	let news = $state(untrack(() => data.marketing?.subscribed ?? false));
	let newsMessage = $state('');
	async function saveNews() {
		newsMessage = '';
		try {
			await saveMarketingChoice(news, 'settings');
			newsMessage = news ? 'You’ll get our news.' : 'You won’t get our news. Booking emails still come as usual.';
		} catch (e) {
			news = !news;
			newsMessage = e instanceof Error ? e.message : 'Your choice could not be saved.';
		}
	}
</script>

<svelte:head><title>Settings — WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/app">← Overview</a>
	<p class="eyebrow">Settings</p>
	<h1 class="page-heading">Account and access.</h1>
	<div class="setting-links">
		<a href="/app/settings/payouts"><span>Payouts</span><b>Where your money is paid ↗</b></a><a
			href="/app/settings/security"><span>Active sessions</span><b>View and revoke ↗</b></a
		><a href="/app/settings/cancellation"><span>Cancellation policy</span><b>Refund rules for buyers ↗</b></a><a
			href="/app/settings/connections"><span>Calendar and meetings</span><b>Google Calendar and Meet ↗</b></a
		><a href="/app/settings/data"><span>Your data</span><b>Download or delete your account ↗</b></a>
	</div>
	<section class="settings-block">
		<h2>Notifications</h2>
		<PushToggle audience="seller" />
	</section>
	<section class="settings-block">
		<h2>News from us</h2>
		{#if data.marketing}<label class="check-line"
				><input type="checkbox" bind:checked={news} onchange={saveNews} /> {marketingWording}</label
			>
			{#if newsMessage}<p class="form-note" aria-live="polite">{newsMessage}</p>{/if}
		{:else}<p class="form-note">Your email preferences could not be loaded. Try again later.</p>{/if}
	</section>
	<div class="notice notice-info">
		Add a payout account under Payouts before taking paid bookings. WantMyTime pays you after each session, once the
		buyer’s time to report a problem has passed.
	</div>
</section>
