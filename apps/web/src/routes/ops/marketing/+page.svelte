<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';

	type Broadcast = {
		id: string;
		subject: string;
		state: 'draft' | 'sending' | 'sent' | 'cancelled';
		created_at: string;
		sent: number;
		waiting: number;
		not_sent: number;
	};
	type Summary = {
		audience: number;
		pending_confirmation: number;
		unsubscribed: number;
		broadcasts: Broadcast[];
		wording: string;
	};
	let summary = $state<Summary | null>(null);
	let message = $state('');
	let busy = $state(false);
	let draft = $state({ subject: '', heading: '', body: '', action_label: '', action_url: '' });
	let unsubscribes = $state('');

	async function load() {
		try {
			summary = await api<Summary>('/api/v1/ops/marketing');
		} catch (e) {
			message = e instanceof Error ? e.message : 'The announcement list could not be loaded.';
		}
	}
	onMount(load);

	async function run(action: () => Promise<string>) {
		busy = true;
		message = '';
		try {
			message = await action();
			await load();
		} catch (e) {
			message = e instanceof Error ? e.message : 'That did not work. Try again.';
		} finally {
			busy = false;
		}
	}

	const create = () =>
		run(async () => {
			await api('/api/v1/ops/broadcasts', { method: 'POST', body: JSON.stringify(draft) });
			draft = { subject: '', heading: '', body: '', action_label: '', action_url: '' };
			return 'Saved as a draft. Send yourself a test before sending it to everyone.';
		});
	const test = (b: Broadcast) =>
		run(async () => {
			const r = await api<{ sent_to: string }>(`/api/v1/ops/broadcasts/${b.id}/test`, { method: 'POST', body: '{}' });
			return `Test sent to ${r.sent_to}.`;
		});
	const send = (b: Broadcast) => {
		const count = summary?.audience ?? 0;
		if (!confirm(`Send “${b.subject}” to ${count} ${count === 1 ? 'person' : 'people'}? This can’t be undone.`)) return;
		return run(async () => {
			const r = await api<{ queued: number }>(`/api/v1/ops/broadcasts/${b.id}/send`, {
				method: 'POST',
				body: JSON.stringify({ confirm_audience: count })
			});
			return `Sending to ${r.queued} people. It goes out a batch at a time; this page shows progress.`;
		});
	};
	const cancel = (b: Broadcast) =>
		run(async () => {
			const r = await api<{ not_sent: number }>(`/api/v1/ops/broadcasts/${b.id}/cancel`, {
				method: 'POST',
				body: '{}'
			});
			return `Stopped. ${r.not_sent} emails were not sent.`;
		});
	const importUnsubscribes = () =>
		run(async () => {
			const emails = unsubscribes.split(/[\s,;]+/).filter(Boolean);
			const r = await api<{ unsubscribed: number; not_found: number }>('/api/v1/ops/marketing/unsubscribes', {
				method: 'POST',
				body: JSON.stringify({ emails })
			});
			unsubscribes = '';
			return `${r.unsubscribed} unsubscribed. ${r.not_found} were not on the list.`;
		});
	const stateLabel = (b: Broadcast) =>
		b.state === 'sending'
			? `Sending · ${b.sent} sent, ${b.waiting} to go`
			: b.state === 'sent'
				? `Sent to ${b.sent}${b.not_sent ? ` · ${b.not_sent} not sent` : ''}`
				: b.state === 'cancelled'
					? `Stopped · ${b.sent} sent`
					: 'Draft';
</script>

<svelte:head><title>Announcements — Ops · WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/ops">← Operations</a>
	<p class="eyebrow">Announcements</p>
	<h1 class="page-heading">News to people who said yes.</h1>
	<p class="page-intro">
		Only people who agreed and whose address is confirmed (a sign-in code, or a paid booking) are emailed. Every email
		carries its own unsubscribe link, and booking emails are never affected.
	</p>
	{#if message}<p class="notice notice-info" aria-live="polite">{message}</p>{/if}
	{#if summary}
		<div class="appointment-slip">
			<div><small>ON THE LIST</small><strong>{summary.audience}</strong></div>
			<div><small>WAITING FOR A PAID BOOKING</small><strong>{summary.pending_confirmation}</strong></div>
			<div><small>UNSUBSCRIBED</small><strong>{summary.unsubscribed}</strong></div>
		</div>
		<p class="form-note">What people agreed to: “{summary.wording}”</p>

		<h2 class="section-heading">Use a mail tool</h2>
		<p class="page-intro">
			Download the list for Brevo, Mailchimp or similar. Each row has that person’s own unsubscribe link. Bring the
			tool’s unsubscribes back here before your next send.
		</p>
		<a class="button button-secondary" href="/api/v1/ops/marketing/contacts.csv" download>Download the list (CSV) ↓</a>
		<form
			class="setup-form"
			onsubmit={(e) => {
				e.preventDefault();
				importUnsubscribes();
			}}
		>
			<label
				>Unsubscribes from your mail tool<textarea
					class="field"
					rows="4"
					bind:value={unsubscribes}
					placeholder="One email address per line"></textarea></label
			>
			<button class="button button-secondary" type="submit" disabled={busy || !unsubscribes.trim()}
				>Mark as unsubscribed</button
			>
		</form>

		<h2 class="section-heading">Write an announcement</h2>
		<form
			class="setup-form"
			onsubmit={(e) => {
				e.preventDefault();
				create();
			}}
		>
			<label>Subject<input class="field" bind:value={draft.subject} maxlength="150" required /></label>
			<label>Heading<input class="field" bind:value={draft.heading} maxlength="150" required /></label>
			<label
				>Message<textarea class="field" rows="8" bind:value={draft.body} maxlength="20000" required></textarea><span
					class="form-note">Leave a blank line between paragraphs.</span
				></label
			>
			<label>Button text · optional<input class="field" bind:value={draft.action_label} maxlength="60" /></label>
			<label
				>Button link · optional<input
					class="field"
					type="url"
					bind:value={draft.action_url}
					placeholder="https://kredit.ng"
				/></label
			>
			<button class="button" type="submit" disabled={busy}>Save draft</button>
		</form>

		<h2 class="section-heading">Announcements</h2>
		{#each summary.broadcasts as b (b.id)}
			<article class="list-card">
				<strong>{b.subject}</strong>
				<p class="form-note">{stateLabel(b)} · {new Date(b.created_at).toLocaleString()}</p>
				<div class="share-actions">
					{#if b.state === 'draft'}
						<button class="button button-secondary" onclick={() => test(b)} disabled={busy}>Send me a test</button>
						<button class="button" onclick={() => send(b)} disabled={busy || summary.audience === 0}
							>Send to {summary.audience} {summary.audience === 1 ? 'person' : 'people'}</button
						>
					{/if}
					{#if b.state === 'draft' || b.state === 'sending'}
						<button class="button button-secondary" onclick={() => cancel(b)} disabled={busy}
							>{b.state === 'draft' ? 'Discard' : 'Stop sending'}</button
						>
					{/if}
				</div>
			</article>
		{:else}
			<p class="form-note">No announcements yet.</p>
		{/each}
	{/if}
</section>
