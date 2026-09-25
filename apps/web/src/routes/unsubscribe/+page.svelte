<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { api } from '$lib/api';

	// Opening the page changes nothing: mail scanners open links on their own.
	// Unsubscribing takes one press of the button.
	const token = page.url.searchParams.get('token') ?? '';
	let email = $state('');
	let subscribed = $state<boolean | null>(null);
	let loading = $state(true);
	let busy = $state(false);
	let message = $state('');

	onMount(async () => {
		try {
			const r = await api<{ email: string; subscribed: boolean }>(
				`/api/v1/marketing/unsubscribe?token=${encodeURIComponent(token)}`
			);
			email = r.email;
			subscribed = r.subscribed;
		} catch (e) {
			message = e instanceof Error ? e.message : 'This link could not be checked.';
		} finally {
			loading = false;
		}
	});

	async function choose(next: boolean) {
		busy = true;
		message = '';
		try {
			await api(`/api/v1/marketing/${next ? 'resubscribe' : 'unsubscribe'}`, {
				method: 'POST',
				body: JSON.stringify({ token })
			});
			subscribed = next;
		} catch (e) {
			message = e instanceof Error ? e.message : 'Your choice could not be saved. Try again.';
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head>
	<title>Email preferences — WantMyTime</title>
	<meta name="robots" content="noindex" />
</svelte:head>
<section class="form-page">
	<p class="eyebrow">Email preferences</p>
	{#if loading}<h1 class="page-heading">One moment.</h1>
	{:else if subscribed === null}<h1 class="page-heading">Link not found.</h1>
		<p class="page-intro">{message} You can also change this under Settings when you sign in.</p>
	{:else if subscribed}<h1 class="page-heading">Stop our news?</h1>
		<p class="page-intro">
			{email} gets news and offers from WantMyTime and Kredit Technologies. Booking and payment emails are separate and keep
			coming either way.
		</p>
		<button class="button" onclick={() => choose(false)} disabled={busy}
			>{busy ? 'Saving…' : 'Unsubscribe'} <span aria-hidden="true">↗</span></button
		>
	{:else}<h1 class="page-heading">You’re unsubscribed.</h1>
		<p class="page-intro">
			{email} won’t get news from WantMyTime or Kredit Technologies. Booking and payment emails still come as usual.
		</p>
		<button class="button button-secondary" onclick={() => choose(true)} disabled={busy}
			>{busy ? 'Saving…' : 'Subscribe again'}</button
		>
	{/if}
	{#if message && subscribed !== null}<p class="notice notice-warning" role="alert">{message}</p>{/if}
</section>
