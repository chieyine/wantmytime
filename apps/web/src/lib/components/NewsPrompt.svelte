<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { marketingWording, saveMarketingChoice } from '$lib/marketing';

	// Asked once, on the dashboard, of sellers who haven't said yes to news.
	// The API decides whether to ask, so a "no" anywhere is never asked again.
	let show = $state(false);
	let answer = $state<'yes' | 'no' | ''>('');
	let busy = $state(false);
	let error = $state('');
	onMount(async () => {
		try {
			show = (await api<{ ask?: boolean }>('/api/v1/me/marketing')).ask === true;
		} catch {
			/* the prompt is optional */
		}
	});
	async function choose(subscribed: boolean) {
		busy = true;
		error = '';
		try {
			await saveMarketingChoice(subscribed, 'dashboard');
			answer = subscribed ? 'yes' : 'no';
		} catch (cause) {
			error = cause instanceof Error ? cause.message : 'Your choice could not be saved.';
		} finally {
			busy = false;
		}
	}
</script>

{#if show}
	<section class="news-prompt" aria-labelledby="news-prompt-title">
		{#if answer === 'yes'}<p class="news-prompt-done" role="status">
				<strong>You’re in.</strong> You can change this any time in <a href="/app/settings">Settings</a>.
			</p>
		{:else if answer === 'no'}<p class="news-prompt-done" role="status">
				No problem. If you change your mind, it’s in <a href="/app/settings">Settings</a>.
			</p>
		{:else}
			<div>
				<p class="workspace-kicker">WANT OUR NEWS?</p>
				<h2 id="news-prompt-title">Hear about new features first.</h2>
				<p>Tips for getting booked and the occasional offer. A few emails a month at most.</p>
			</div>
			<div class="news-prompt-actions">
				<button class="button" type="button" onclick={() => choose(true)} disabled={busy}>Yes, send me news</button>
				<button class="button button-secondary" type="button" onclick={() => choose(false)} disabled={busy}
					>No thanks</button
				>
				<small>{marketingWording}</small>
			</div>
			{#if error}<p class="notice notice-warning" role="alert">{error}</p>{/if}
		{/if}
	</section>
{/if}
