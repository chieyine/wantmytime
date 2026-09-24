<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { formatNaira } from '$lib/money';
	import CursorPager from '$lib/components/CursorPager.svelte';
	type Item = { id:string; kind:string; state:string; booking_id:string|null; payment_attempt_id:string|null; provider_deadline:string|null; amount_minor:number|null; reason:string; created_at:string };
	let items=$state<Item[]>([]);let message=$state('');let loading=$state(true);let nextCursor=$state('');
	async function load(cursor=''){loading=true;message='';try{const q=cursor?`?cursor=${encodeURIComponent(cursor)}`:'';const page=await api<{exceptions:Item[];next_cursor:string}>(`/api/v1/ops/exceptions${q}`);items=cursor?[...items,...page.exceptions]:page.exceptions;nextCursor=page.next_cursor}catch(e){message=e instanceof Error?e.message:'Payment exceptions could not be loaded.'}finally{loading=false}}
	onMount(()=>load());
</script>
<svelte:head><title>Exceptions — Ops · WantMyTime</title></svelte:head>
<section class="form-page app-page"><a class="back-link" href="/ops">← Operations</a><p class="eyebrow">Payment exceptions</p><h1 class="page-heading">Objective cases for review.</h1>
{#if message}<div class="notice notice-warning">{message}</div>{/if}
{#if loading&&!items.length}<p class="page-intro">Loading exception records…</p>{:else if items.length}<div class="list-stack">{#each items as item}<a class="list-card" href={`/ops/exceptions/${encodeURIComponent(item.id)}`}><div><strong>{item.kind.replaceAll('_',' ')} · {item.state.replaceAll('_',' ')}</strong><p>{item.reason}</p><small>{item.booking_id?`Booking ${item.booking_id}`:''} {item.payment_attempt_id?`Payment ${item.payment_attempt_id}`:''}</small>{#if item.provider_deadline}<small>Provider deadline {new Date(item.provider_deadline).toLocaleString()}</small>{/if}</div>{#if item.amount_minor!==null}<b>{formatNaira(item.amount_minor)}</b>{/if}</a>{/each}</div><CursorPager cursor={nextCursor} busy={loading} onNext={()=>load(nextCursor)}/>{:else if !loading}<p class="page-intro">No open payment exceptions have been recorded.</p><div class="notice notice-info">Booking delivery issues and cancellation requests are tracked in their own queues. Provider exceptions appear here when a verified payment pipeline creates them.</div>{/if}</section>
