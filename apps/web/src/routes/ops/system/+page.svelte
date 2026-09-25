<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	type Market = { country: string; currency: string; checkout_ready: boolean; payment_methods: string[] };
	type Status = {
		email_transport_configured: boolean;
		payments_enabled: boolean;
		markets?: Market[];
		notifications: {
			queued: number;
			processing: number;
			sent: number;
			failed: number;
			cancelled: number;
			oldest_queued_at: string | null;
		};
		provider_events: { queued: number; processing: number; failed: number; oldest_pending_at: string | null };
	};
	type Alert = {
		key: string;
		title: string;
		state: 'firing' | 'resolved';
		severity: 'critical' | 'warning';
		summary: string;
		first_fired_at: string;
		last_notified_at: string | null;
		resolved_at: string | null;
		updated_at: string;
	};
	type Worker = {
		name: string;
		instance: string;
		last_beat_at: string;
		age_seconds: number;
		last_error_at: string | null;
		last_error: string | null;
	};
	type Alerts = {
		alerts: Alert[];
		workers: Worker[];
		alert_recipients_configured: boolean;
		error_reporting_configured: boolean;
	};
	let status = $state<Status | null>(null);
	let message = $state('');
	let alerts = $state<Alerts | null>(null);
	let alertsMessage = $state('');
	const firing = $derived(alerts?.alerts.filter((a) => a.state === 'firing') ?? []);
	const recent = $derived(alerts?.alerts.filter((a) => a.state === 'resolved').slice(0, 10) ?? []);
	// Matches the watchdog: request workers cycle every few seconds, the backup job daily.
	const staleAfter = (name: string) => (name === 'backup' ? 26 * 3600 : 180);
	function age(seconds: number) {
		if (seconds > 365 * 86400) return 'never';
		if (seconds < 90) return `${Math.round(seconds)}s ago`;
		if (seconds < 5400) return `${Math.round(seconds / 60)} min ago`;
		return `${Math.round(seconds / 3600)} h ago`;
	}
	onMount(async () => {
		const [s, a] = await Promise.allSettled([api<Status>('/api/v1/ops/system'), api<Alerts>('/api/v1/ops/alerts')]);
		if (s.status === 'fulfilled') status = s.value;
		else message = s.reason instanceof Error ? s.reason.message : 'System status could not be loaded.';
		if (a.status === 'fulfilled') alerts = a.value;
		else alertsMessage = a.reason instanceof Error ? a.reason.message : 'Alerts could not be loaded.';
	});
</script>

<svelte:head><title>System health — Ops · WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/ops">← Operations</a>
	<p class="eyebrow">System health</p>
	<h1 class="page-heading">System health.</h1>
	{#if status?.markets?.length}
		<h2 class="section-heading">Seller countries</h2>
		<div class="list-stack">
			{#each status.markets as m (m.country)}<article class="list-card">
					<div>
						<strong>{m.country} · {m.currency}</strong>
						<p>
							{m.checkout_ready
								? `Taking payments: ${m.payment_methods.join(', ')}`
								: 'Not taking payments: set its payment channels and limits.'}
						</p>
					</div>
					<span class:alert-critical={!m.checkout_ready}>{m.checkout_ready ? 'READY' : 'OFF'}</span>
				</article>{/each}
		</div>
	{/if}
	{#if alerts}
		<h2 class="section-heading">Alerts</h2>
		{#if firing.length === 0}<div class="notice notice-info">No alerts are firing.</div>{:else}<div class="list-stack">
				{#each firing as al (al.key)}<article class="list-card">
						<div>
							<strong>{al.title}</strong>
							<p>{al.summary}</p>
							<small
								>Firing since {new Date(al.first_fired_at).toLocaleString()}{al.last_notified_at
									? ` · last emailed ${new Date(al.last_notified_at).toLocaleString()}`
									: ''}</small
							>
						</div>
						<span class:alert-critical={al.severity === 'critical'}>{al.severity.toUpperCase()}</span>
					</article>{/each}
			</div>{/if}
		{#if !alerts.alert_recipients_configured}<div class="notice notice-warning">
				ALERT_EMAILS is not set, so alerts are only visible on this page.
			</div>{/if}
		{#if !alerts.error_reporting_configured}<p class="form-note">
				Error reporting is off (SENTRY_DSN is not set). Errors are still written to the API logs.
			</p>{/if}
		<h2 class="section-heading">Background workers</h2>
		{#if alerts.workers.length === 0}<p class="page-intro">No worker has reported in yet.</p>{:else}<div
				class="list-stack"
			>
				{#each alerts.workers as wk (wk.name)}<article class="list-card">
						<div>
							<strong>{wk.name.replaceAll('_', ' ')}</strong>
							<p>
								{#if age(wk.age_seconds) === 'never'}{wk.name === 'backup'
										? 'No verified backup yet'
										: 'Never completed a cycle'}{:else}{wk.name === 'backup' ? 'Last verified backup' : 'Last check-in'}
									{age(wk.age_seconds)}{/if} · {wk.instance}
							</p>
							{#if wk.last_error && wk.last_error_at}<small
									>Last error {new Date(wk.last_error_at).toLocaleString()}: {wk.last_error}</small
								>{/if}
						</div>
						<span class:alert-critical={wk.age_seconds > staleAfter(wk.name)}
							>{wk.age_seconds > staleAfter(wk.name) ? 'STALE' : 'OK'}</span
						>
					</article>{/each}
			</div>{/if}
		{#if recent.length > 0}<details class="form-note">
				<summary>Recently resolved ({recent.length})</summary>
				<ul>
					{#each recent as al (al.key)}<li>
							{al.title} · resolved {al.resolved_at ? new Date(al.resolved_at).toLocaleString() : ''}
						</li>{/each}
				</ul>
			</details>{/if}
	{:else if alertsMessage}<div class="notice notice-warning" role="status">{alertsMessage}</div>{/if}
	{#if status}<h2 class="section-heading">Email delivery</h2>
		<p class="page-intro">
			{status.email_transport_configured ? 'An email transport is configured.' : 'No email transport is configured.'} Delivery
			jobs are persisted and retried; queued counts include scheduled reminders.
		</p>
		<div class="appointment-slip">
			<div><small>QUEUED</small><strong>{status.notifications.queued}</strong></div>
			<div><small>PROCESSING</small><strong>{status.notifications.processing}</strong></div>
			<div><small>FAILED</small><strong>{status.notifications.failed}</strong></div>
			<div><small>SENT</small><strong>{status.notifications.sent}</strong></div>
		</div>
		{#if status.notifications.oldest_queued_at}<p class="form-note">
				Next queued message due {new Date(status.notifications.oldest_queued_at).toLocaleString()}.
			</p>{/if}
		{#if status.notifications.failed > 0}<div class="notice notice-warning">
				Some notification deliveries reached the retry limit. Review API logs and restore the configured sender before
				asking users to retry access.
			</div>{:else}<div class="notice notice-info">No notification deliveries have reached the retry limit.</div>{/if}
		<h2 class="section-heading">Provider payment events</h2>
		<p class="page-intro">
			Collection is {status.payments_enabled ? 'enabled by configured environment gates' : 'disabled'}. Signed webhook
			events are verified with the provider before any booking or allocation is recorded.
		</p>
		<div class="appointment-slip">
			<div><small>QUEUED / RETRY</small><strong>{status.provider_events.queued}</strong></div>
			<div><small>PROCESSING</small><strong>{status.provider_events.processing}</strong></div>
			<div><small>FAILED</small><strong>{status.provider_events.failed}</strong></div>
		</div>
		<a class="button button-secondary" href="/ops/provider-events">Review provider event inbox ↗</a
		>{#if status.provider_events.oldest_pending_at}<p class="form-note">
				Oldest outstanding provider event: {new Date(status.provider_events.oldest_pending_at).toLocaleString()}.
			</p>{/if}{#if status.provider_events.failed > 0}<div class="notice notice-warning">
				Provider events exhausted automatic retries. Review them and record an audited retry reason if appropriate.
			</div>{/if}
	{:else}<div class="notice notice-warning" role="status">{message || 'Loading system health…'}</div>{/if}
</section>
