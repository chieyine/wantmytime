<script lang="ts">
	import { env } from '$env/dynamic/public';
	import { recordProductEvent } from '$lib/analytics';
	let { data } = $props();
	// The workspace layout already knows the seller's link.
	let handle = $derived((data.account as { handle: string }).handle ?? '');
	const appOrigin = (env.PUBLIC_APP_ORIGIN || window.location.origin).replace(/\/$/, '');
	let done = $state(false);
	let message = $state('');
	let url = $derived(handle ? `${appOrigin}/${encodeURIComponent(handle)}` : '');
	async function copy() {
		try {
			await navigator.clipboard.writeText(url);
			done = true;
			message = 'Link copied.';
			void recordProductEvent('link_copy_clicked', handle);
		} catch {
			message = 'Clipboard access is unavailable. Select and copy your link above.';
		}
	}
	async function share() {
		if (!url) return;
		void recordProductEvent('share_action_opened', handle);
		try {
			if (navigator.share)
				await navigator.share({
					title: 'Book time with me',
					text: 'Happy to help. I take calls through my link:',
					url
				});
			else await copy();
		} catch (e) {
			if (e instanceof Error && e.name !== 'AbortError')
				message = 'The share sheet could not open. You can copy the link instead.';
		}
	}
</script>

<svelte:head><title>Share your link — WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/app">← Overview</a>
	<p class="eyebrow">Share</p>
	<h1 class="page-heading">Put it everywhere.</h1>
	{#if !handle}<div class="notice notice-warning" role="alert">
			You haven’t claimed a link yet. <a class="text-link" href="/claim">Claim your link ↗</a>
		</div>{:else}<div class="share-card">
			<small>YOUR LINK</small><strong><a href={url}>{url}</a></strong>
			<p>“Can I pick your brain?”<br />“Happy to. Book a time here.”</p>
			<button class="text-link" onclick={copy}>Copy link</button>
		</div>
		<div class="share-actions">
			<button class="button" onclick={share}>Share link ↗</button><a class="button button-secondary" href={url}
				>Open public page ↗</a
			>
		</div>
		{#if done}<p class="notice notice-info" aria-live="polite">Link copied.</p>{/if}{#if message && !done}<p
				class="notice notice-info"
				aria-live="polite"
			>
				{message}
			</p>{/if}
		<section class="readiness-panel">
			<h2>Where it works best</h2>
			<ul>
				<li>Your Instagram, X, TikTok and LinkedIn bio</li>
				<li>Your email signature</li>
				<li>Your reply to every “quick call?” and “can I pick your brain?”</li>
				<li>Your WhatsApp status and Linktree</li>
			</ul>
			<p class="form-note">
				Anyone who asks for your time: a client, a follower, a colleague of a colleague. Your call who gets it.
			</p>
		</section>
		<section class="readiness-panel">
			<h2>A reply you can paste</h2>
			<p>Happy to help. I take calls through my link: {url}</p>
			<button
				class="text-link"
				onclick={async () => {
					try {
						await navigator.clipboard.writeText(`Happy to help. I take calls through my link: ${url}`);
						message = 'Reply copied.';
						done = false;
					} catch {
						message = 'Clipboard access is unavailable. Select and copy the reply above.';
					}
				}}>Copy reply</button
			>
		</section>{/if}
</section>
