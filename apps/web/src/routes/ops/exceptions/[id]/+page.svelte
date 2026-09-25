<script lang="ts">
	import { onMount } from 'svelte';
	import { api, API } from '$lib/api';
	import { formatMoney } from '$lib/money';
	let { params } = $props();
	type Item = {
		id: string;
		kind: string;
		state: string;
		booking_id: string | null;
		payment_attempt_id: string | null;
		provider_case_reference: string | null;
		provider_deadline: string | null;
		amount_minor: number | null;
		currency: string | null;
		reason: string;
		evidence: Record<string, unknown>;
		resolution: string | null;
		created_at: string;
		resolved_at: string | null;
	};
	let item = $state<Item | null>(null);
	let resolution = $state('');
	let message = $state('');
	let loading = $state(true);
	let busy = $state(false);
	async function load() {
		try {
			item = await api<Item>(`/api/v1/ops/exceptions/${encodeURIComponent(params.id)}`);
		} catch (e) {
			message = e instanceof Error ? e.message : 'The exception could not be loaded.';
		} finally {
			loading = false;
		}
	}
	onMount(load);
	async function resolve() {
		busy = true;
		message = '';
		try {
			await api(`/api/v1/ops/exceptions/${encodeURIComponent(params.id)}/resolve`, {
				method: 'POST',
				body: JSON.stringify({ resolution })
			});
			message = 'Review outcome recorded and audited. Payment and settlement records were not changed.';
			resolution = '';
			await load();
		} catch (e) {
			message = e instanceof Error ? e.message : 'The exception could not be resolved.';
		} finally {
			busy = false;
		}
	}
	async function exportEvidence() {
		busy = true;
		message = '';
		try {
			const response = await fetch(`${API}/api/v1/ops/exceptions/${encodeURIComponent(params.id)}/export`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' }
			});
			if (!response.ok) {
				const body = await response.json().catch(() => null);
				throw new Error(body?.error?.message || 'Evidence export could not be created.');
			}
			const blob = await response.blob();
			const url = URL.createObjectURL(blob);
			const link = document.createElement('a');
			link.href = url;
			link.download = `aside-payment-exception-${params.id}.json`;
			link.click();
			URL.revokeObjectURL(url);
			message = 'Minimized evidence exported. The access was recorded in the audit log.';
		} catch (e) {
			message = e instanceof Error ? e.message : 'Evidence export could not be created.';
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>Exception detail — Ops · WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/ops/exceptions">← Exceptions</a>
	<p class="eyebrow">Payment exception</p>
	{#if loading}<p class="page-intro">Loading record…</p>{:else if !item}<h1 class="page-heading">
			Record unavailable.
		</h1>
		<p class="page-intro">{message}</p>{:else}<h1 class="page-heading">{item.kind.replaceAll('_', ' ')}.</h1>
		<button class="button button-secondary" onclick={exportEvidence} disabled={busy}>Export minimized evidence</button>
		<p class="form-note">
			The export omits raw webhook bodies, buyer contact details, credentials, and meeting links. Export access is
			audited.
		</p>
		<div class="appointment-slip">
			<div><small>STATE</small><strong>{item.state.replaceAll('_', ' ')}</strong></div>
			<div><small>CREATED</small><strong>{new Date(item.created_at).toLocaleString()}</strong></div>
			{#if item.amount_minor !== null}<div>
					<small>AMOUNT</small><strong>{formatMoney(item.amount_minor, item.currency ?? 'NGN')} {item.currency}</strong>
				</div>{/if}
			<div><small>PROVIDER CASE</small><strong>{item.provider_case_reference || 'Not provided'}</strong></div>
		</div>
		<p>{item.reason}</p>
		{#if item.provider_deadline}<div class="notice notice-warning">
				Provider deadline: {new Date(item.provider_deadline).toLocaleString()}
			</div>{/if}{#if item.booking_id}<p>
				Booking: <a class="text-link" href={`/ops/bookings/${encodeURIComponent(item.booking_id)}`}>{item.booking_id}</a
				>
			</p>{/if}{#if item.payment_attempt_id}<p>
				Payment attempt: <a class="text-link" href={`/ops/payments/${encodeURIComponent(item.payment_attempt_id)}`}
					>{item.payment_attempt_id}</a
				>
			</p>{/if}{#if item.evidence}<pre class="evidence-block">{JSON.stringify(
					item.evidence,
					null,
					2
				)}</pre>{/if}{#if item.state !== 'resolved'}<div class="setup-form">
				<label>Review outcome<textarea class="field" rows="4" bind:value={resolution} maxlength="500"></textarea></label
				>
				<p class="form-note">
					This records an audited review only. It does not refund, reroute, or change a payment or booking.
				</p>
				<button class="button" onclick={resolve} disabled={busy || resolution.trim().length < 8}
					>{busy ? 'Saving…' : 'Record review outcome'}</button
				>
			</div>{:else}<div class="notice notice-info">
				Resolved {item.resolved_at ? new Date(item.resolved_at).toLocaleString() : ''}: {item.resolution}
			</div>{/if}{#if message}<p class="notice notice-info" aria-live="polite">{message}</p>{/if}{/if}
</section>
