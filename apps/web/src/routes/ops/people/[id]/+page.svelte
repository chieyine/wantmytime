<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	let { params } = $props();
	type Person = { id: string; name: string; status: string; email: string | null; created_at: string; seller: { handle: string; publication_state: string; readiness_state: string; paused: boolean; payout_account_added: boolean } | null };
	let person = $state<Person | null>(null);
	let message = $state('');
	let actionMessage = $state('');
	let reason = $state('');
	let busy = $state(false);
	async function load() {
		try { person = await api<Person>(`/api/v1/ops/people/${encodeURIComponent(params.id)}`); }
		catch (e) { message = e instanceof Error ? e.message : 'Account could not be loaded.'; }
	}
	onMount(load);
	async function setReadiness(ready: boolean) {
		busy = true; actionMessage = '';
		try {
			await api(`/api/v1/ops/people/${encodeURIComponent(params.id)}/payout-readiness`, { method: 'POST', body: JSON.stringify({ ready, reason }) });
			actionMessage = ready ? 'Seller marked ready. The change is audited.' : 'Seller taken out of paid bookings. The change is audited.';
			reason = '';
			await load();
		} catch (e) {
			actionMessage = e instanceof Error ? e.message : 'Payout readiness could not be updated.';
		} finally { busy = false; }
	}
</script>
<svelte:head><title>Person detail — Ops · WantMyTime</title></svelte:head>
<section class="form-page app-page"><a class="back-link" href="/ops/people">← People</a><p class="eyebrow">Account record</p>
{#if person}<h1 class="page-heading">{person.name}</h1><div class="list-stack"><article class="list-card"><div><strong>Account</strong><p>{person.email || 'No verified email'} · {person.status}</p><small>Created {new Date(person.created_at).toLocaleString()}</small></div></article>
{#if person.seller}<article class="list-card"><div><strong>Personal link</strong><p>@{person.seller.handle} · {person.seller.publication_state}</p><small>Readiness: {person.seller.readiness_state} · {person.seller.paused ? 'Paused' : 'Taking bookings'} · Bank account {person.seller.payout_account_added ? 'added' : 'not added yet'}</small></div></article>
<section class="readiness-panel"><h2>Payout readiness</h2><p>Mark this seller ready only after their identity checks are complete and they have added a bank account (they do this under Settings › Payouts; the account name is checked with the bank). Requires the <code>ops:seller:approve</code> permission.</p><div class="setup-form"><label>Evidence reviewed<textarea class="field" rows="3" maxlength="300" bind:value={reason}></textarea></label><div class="ops-nav"><button class="button" onclick={() => setReadiness(true)} disabled={busy || reason.trim().length < 8}>Mark ready for paid bookings</button><button class="button button-secondary" onclick={() => setReadiness(false)} disabled={busy || reason.trim().length < 8}>Mark not ready</button></div>{#if actionMessage}<p class="notice notice-info" aria-live="polite">{actionMessage}</p>{/if}</div></section>
{:else}<p class="page-intro">This account has not claimed a seller link.</p>{/if}</div>
{:else if message}<div class="notice notice-warning" role="status">{message}</div>{:else}<p class="page-intro">Loading account…</p>{/if}</section>
