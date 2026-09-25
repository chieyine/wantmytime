<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import CursorPager from '$lib/components/CursorPager.svelte';
	type Request = {
		id: string;
		booking_id: string;
		reason: string;
		created_at: string;
		starts_at: string;
		seller: string;
		requester: string;
	};
	let requests = $state<Request[]>([]);
	let resolutions = $state<Record<string, string>>({});
	let message = $state('');
	let nextCursor = $state('');
	let loading = $state(false);
	async function load(cursor = '') {
		loading = true;
		try {
			const q = cursor ? `?cursor=${encodeURIComponent(cursor)}` : '';
			const page = await api<{ requests: Request[]; next_cursor: string }>(`/api/v1/ops/cancellations${q}`);
			requests = cursor ? [...requests, ...page.requests] : page.requests;
			nextCursor = page.next_cursor;
		} catch (e) {
			message = e instanceof Error ? e.message : 'Cancellation requests could not be loaded.';
		} finally {
			loading = false;
		}
	}
	onMount(load);
	async function resolve(id: string) {
		message = '';
		try {
			await api(`/api/v1/ops/cancellations/${encodeURIComponent(id)}/resolve`, {
				method: 'POST',
				body: JSON.stringify({ resolution: resolutions[id] || '' })
			});
			message = 'Review outcome recorded and audited. Booking and payment states were not changed.';
			await load();
		} catch (e) {
			message = e instanceof Error ? e.message : 'The request could not be resolved.';
		}
	}
</script>

<svelte:head><title>Cancellation requests — Ops · WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/ops">← Operations</a>
	<p class="eyebrow">Cancellations</p>
	<h1 class="page-heading">Requests for review.</h1>
	{#if message}<p class="notice notice-info" aria-live="polite">{message}</p>{/if}{#if requests.length}<div
			class="list-stack"
		>
			{#each requests as item}<article class="list-card">
					<div>
						<strong>{item.requester} · {item.seller}</strong>
						<p>Booking {item.booking_id} · {new Date(item.starts_at).toLocaleString()}</p>
						<p>{item.reason}</p>
						<small>Requested {new Date(item.created_at).toLocaleString()}</small><label
							>Review outcome<input class="field" bind:value={resolutions[item.id]} /></label
						><button
							class="button button-secondary"
							onclick={() => resolve(item.id)}
							disabled={(resolutions[item.id] || '').trim().length < 8}>Record review outcome</button
						>
					</div>
				</article>{/each}
		</div>
		<CursorPager cursor={nextCursor} busy={loading} onNext={() => load(nextCursor)} />{:else if !loading && !message}<p
			class="page-intro"
		>
			No open cancellation requests.
		</p>{:else if loading}<p class="page-intro">Loading cancellation requests…</p>{/if}
</section>
