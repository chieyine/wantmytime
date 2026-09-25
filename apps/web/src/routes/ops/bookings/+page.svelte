<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { formatMoney } from '$lib/money';
	import CursorPager from '$lib/components/CursorPager.svelte';
	type Booking = {
		id: string;
		seller: string;
		buyer_name: string;
		duration_minutes: number;
		starts_at: string;
		gross_minor: string;
		currency?: string;
		state: string;
		payment_state: string;
		issue_reason: string | null;
		issue_resolved: boolean;
	};
	let bookings = $state<Booking[]>([]);
	let resolutions = $state<Record<string, string>>({});
	let message = $state('');
	let nextCursor = $state('');
	let loading = $state(false);
	async function load(cursor = '') {
		loading = true;
		try {
			const q = cursor ? `?cursor=${encodeURIComponent(cursor)}` : '';
			const page = await api<{ bookings: Booking[]; next_cursor: string }>(`/api/v1/ops/bookings${q}`);
			bookings = cursor ? [...bookings, ...page.bookings] : page.bookings;
			nextCursor = page.next_cursor;
		} catch (e) {
			message = e instanceof Error ? e.message : 'Bookings could not be loaded.';
		} finally {
			loading = false;
		}
	}
	onMount(() => load());
	async function resolve(id: string) {
		message = '';
		try {
			await api(`/api/v1/ops/bookings/${encodeURIComponent(id)}/resolve-issue`, {
				method: 'POST',
				body: JSON.stringify({ resolution: resolutions[id] || '' })
			});
			message = 'Issue resolution recorded and audited.';
			await load();
		} catch (e) {
			message = e instanceof Error ? e.message : 'Issue could not be resolved.';
		}
	}
</script>

<svelte:head><title>Bookings — Ops · WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/ops">← Operations</a>
	<p class="eyebrow">Bookings</p>
	<h1 class="page-heading">Conversation records.</h1>
	{#if message}<p class="notice notice-info" aria-live="polite">{message}</p>{/if}{#if bookings.length}<div
			class="list-stack"
		>
			{#each bookings as b}<article class="list-card">
					<div>
						<strong>{b.buyer_name} · {b.seller}</strong>
						<p>{b.duration_minutes} minutes · {new Date(b.starts_at).toLocaleString()}</p>
						<small>{b.state} · {b.payment_state}</small>{#if b.issue_reason}<p class="notice notice-warning">
								Issue: {b.issue_reason}{b.issue_resolved ? ' · resolved' : ''}
							</p>
							{#if !b.issue_resolved}<label
									>Resolution notes<input class="field" bind:value={resolutions[b.id]} /></label
								><button class="button button-secondary" onclick={() => resolve(b.id)}>Record resolution</button
								>{/if}{/if}
					</div>
					<strong>{formatMoney(Number(b.gross_minor), b.currency)}</strong>
				</article>{/each}
		</div>
		<CursorPager cursor={nextCursor} busy={loading} onNext={() => load(nextCursor)} />{:else if !loading && !message}<p
			class="page-intro"
		>
			No bookings have been recorded.
		</p>{/if}
</section>
