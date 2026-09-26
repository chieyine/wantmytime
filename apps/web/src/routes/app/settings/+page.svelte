<script lang="ts">
	import { untrack } from 'svelte';
	import { api } from '$lib/api';
	import PushToggle from '$lib/components/PushToggle.svelte';
	import { marketingWording, saveMarketingChoice } from '$lib/marketing';
	let { data } = $props();
	let news = $state(untrack(() => data.marketing?.subscribed ?? false));
	let newsMessage = $state('');
	let signingOut = $state(false);
	let signOutMessage = $state('');
	let calendar = $derived(data.calendar);
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
	async function signOut() {
		signingOut = true;
		signOutMessage = '';
		try {
			await api('/api/v1/auth/logout', { method: 'POST' });
			window.location.href = '/login';
		} catch {
			signOutMessage = 'Could not sign out. Please try again.';
			signingOut = false;
		}
	}
	// Ends every session, this one included.
	async function signOutEverywhere() {
		signingOut = true;
		signOutMessage = '';
		try {
			await api('/api/v1/me/sessions/revoke-others', { method: 'POST', body: '{}' });
			await api('/api/v1/auth/logout', { method: 'POST' });
			window.location.href = '/login';
		} catch (e) {
			signOutMessage = e instanceof Error ? e.message : 'Could not sign out everywhere. Please try again.';
			signingOut = false;
		}
	}
</script>

<svelte:head><title>Settings — WantMyTime</title></svelte:head>
<section class="form-page app-page settings-page">
	<a class="back-link" href="/app">← Home</a>
	<p class="eyebrow">Settings</p>
	<h1 class="page-heading">Settings.</h1>
	<section class="settings-block">
		<h2>Notifications</h2>
		<PushToggle audience="seller" />
	</section>
	<section class="settings-block">
		<h2>Calendar</h2>
		<a class="settings-row" href="/app/settings/connections"
			><span
				>Google Calendar <small
					>{calendar?.connected
						? `Connected${calendar.account_email ? ` · ${calendar.account_email}` : ''}`
						: 'Block busy times and add bookings to your calendar'}</small
				></span
			><b>{calendar?.connected ? 'Manage' : 'Connect'} ↗</b></a
		>
	</section>
	<section class="settings-block">
		<h2>News from us</h2>
		{#if data.marketing}<label class="check-line"
				><input type="checkbox" bind:checked={news} onchange={saveNews} /> {marketingWording}</label
			>
			{#if newsMessage}<p class="form-note" aria-live="polite">{newsMessage}</p>{/if}
		{:else}<p class="form-note">Your email preferences could not be loaded. Try again later.</p>{/if}
	</section>
	<section class="settings-block">
		<h2>Sign-in</h2>
		<p class="form-note">
			Lost a phone or used a shared computer? “All devices” signs you out everywhere, this one too.
		</p>
		<div class="settings-signout">
			<button class="button" type="button" onclick={signOut} disabled={signingOut}>Sign out</button><button
				class="button button-secondary"
				type="button"
				onclick={signOutEverywhere}
				disabled={signingOut}>Sign out on all devices</button
			>
		</div>
		{#if signOutMessage}<p class="notice notice-warning" role="alert">{signOutMessage}</p>{/if}
	</section>
	<p class="settings-quiet-links">
		<a href="/api/v1/me/data-export" download>Download your data</a><a href="/app/settings/data">Delete your account</a>
	</p>
</section>
