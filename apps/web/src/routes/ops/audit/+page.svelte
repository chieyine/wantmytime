<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import CursorPager from '$lib/components/CursorPager.svelte';
	type Event = {
		id: string;
		actor_id: string | null;
		action: string;
		target_id: string | null;
		reason: string | null;
		summary: Record<string, unknown>;
		created_at: string;
	};
	let events = $state<Event[]>([]);
	let message = $state('');
	let cursor = $state('');
	let loading = $state(false);
	async function load(after = '') {
		loading = true;
		try {
			const q = after ? `?cursor=${encodeURIComponent(after)}` : '';
			const page = await api<{ events: Event[]; next_cursor: string }>(`/api/v1/ops/audit${q}`);
			events = after ? [...events, ...page.events] : page.events;
			cursor = page.next_cursor;
		} catch (e) {
			message = e instanceof Error ? e.message : 'Audit history could not be loaded.';
		} finally {
			loading = false;
		}
	}
	onMount(() => load());
</script>

<svelte:head><title>Audit — Ops · WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/ops">← Operations</a>
	<p class="eyebrow">Audit</p>
	<h1 class="page-heading">A record of sensitive actions.</h1>
	{#if message}<p class="notice notice-warning">{message}</p>{/if}{#if events.length}<div class="list-stack">
			{#each events as event}<article class="list-card">
					<div>
						<strong>{event.action}</strong>
						<p>{event.reason || 'System event'} · target {event.target_id || '—'}</p>
						<small>{new Date(event.created_at).toLocaleString()} · actor {event.actor_id || 'system'}</small>
					</div>
				</article>{/each}
		</div>
		<CursorPager {cursor} busy={loading} onNext={() => load(cursor)} />{:else if !loading && !message}<p
			class="page-intro"
		>
			No audit events have been recorded.
		</p>{:else}<p class="page-intro">Loading audit history…</p>{/if}
</section>
