<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';

	type Queue = { queued: number; processing: number; failed: number };
	type Status = {
		email_transport_configured: boolean;
		payments_enabled: boolean;
		checkouts_paused: boolean;
		notifications: Queue;
		provider_events: Queue;
	};
	let status = $state<Status | null>(null);
	let message = $state('');
	onMount(async () => {
		try { status = await api<Status>('/api/v1/ops/system'); }
		catch (error) { message = error instanceof Error ? error.message : 'Operational settings could not be loaded.'; }
	});
</script>

<svelte:head><title>Operations settings — WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/ops">← Operations</a>
	<p class="eyebrow">Settings and safeguards</p>
	<h1 class="page-heading">Controlled at the service boundary.</h1>
	<p class="page-intro">Financial and provider settings are deployed as protected server configuration. This screen is read-only: operators cannot change fee terms, routes, or collection approval from the browser.</p>
	{#if status}
		<div class="appointment-slip">
			<div><small>NEW CHECKOUTS</small><strong>{status.checkouts_paused ? 'Paused' : status.payments_enabled ? 'Enabled' : 'Disabled'}</strong></div>
			<div><small>PAYMENT GATE</small><strong>{status.payments_enabled ? 'All configured gates pass' : 'Not enabled'}</strong></div>
			<div><small>EMAIL TRANSPORT</small><strong>{status.email_transport_configured ? 'Configured' : 'Not configured'}</strong></div>
		</div>
		{#if status.checkouts_paused}
			<div class="notice notice-warning">The emergency pause blocks new provider checkout initialization. Provider events already received continue through verification and recovery.</div>
		{:else if !status.payments_enabled}
			<div class="notice notice-info">New provider checkouts are disabled because the server-side payment approval gates are not all satisfied.</div>
		{:else}
			<div class="notice notice-warning">The configured server-side gates permit new checkouts. This status does not itself prove owner, legal, or provider approval; review the approval record before operating.</div>
		{/if}
		<h2 class="section-heading">Operational queues</h2>
		<div class="appointment-slip">
			<div><small>EMAIL QUEUED</small><strong>{status.notifications.queued}</strong></div>
			<div><small>EMAIL PROCESSING</small><strong>{status.notifications.processing}</strong></div>
			<div><small>EMAIL FAILED</small><strong>{status.notifications.failed}</strong></div>
			<div><small>PROVIDER QUEUED</small><strong>{status.provider_events.queued}</strong></div>
			<div><small>PROVIDER PROCESSING</small><strong>{status.provider_events.processing}</strong></div>
			<div><small>PROVIDER FAILED</small><strong>{status.provider_events.failed}</strong></div>
		</div>
		<nav class="ops-nav"><a href="/ops/system">System health ↗</a><a href="/ops/provider-events">Provider event inbox ↗</a><a href="/ops/audit">Audit history ↗</a></nav>
		<p class="form-note">To change a protected gate, update the deployment’s secret configuration through its approved release process. Keep <code>CHECKOUTS_PAUSED=true</code> during an incident; this pause does not stop processing existing provider events.</p>
	{:else}
		<div class="notice notice-warning" role="status">{message || 'Loading protected configuration status…'}</div>
		{#if message}<a class="button button-secondary" href="/ops/access">Verify operations access ↗</a>{/if}
	{/if}
</section>
