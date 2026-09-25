<script lang="ts">
	import { api } from '$lib/api';
	import type { Session } from './+page';
	let { data } = $props();
	let sessions = $derived(data.sessions);
	let busy = $state(false);
	let message = $derived(data.loadError);
	async function load() {
		try {
			sessions = (await api<{ sessions: Session[] }>('/api/v1/me/sessions')).sessions;
		} catch (e) {
			message = e instanceof Error ? e.message : 'Sessions could not be loaded.';
		}
	}
	async function revokeOthers() {
		busy = true;
		message = '';
		try {
			const result = await api<{ revoked_sessions: number }>('/api/v1/me/sessions/revoke-others', {
				method: 'POST',
				body: '{}'
			});
			message = `${result.revoked_sessions} other session${result.revoked_sessions === 1 ? '' : 's'} revoked.`;
			await load();
		} catch (e) {
			message = e instanceof Error ? e.message : 'Other sessions could not be revoked.';
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>Security — WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/app/settings">← Settings</a>
	<p class="eyebrow">Security</p>
	<h1 class="page-heading">Your active sessions.</h1>
	<p class="page-intro">Sign out of other browsers if you no longer use them. This session stays active.</p>
	{#if message}<p class="notice notice-info" aria-live="polite">{message}</p>{/if}{#if sessions.length}<div
			class="list-stack"
		>
			{#each sessions as session, i (`${session.created_at}-${i}`)}<article class="list-card">
					<div>
						<strong>{session.current ? 'This session' : 'Signed-in session'}</strong>
						<p>Started {new Date(session.created_at).toLocaleString()}</p>
						<small>Expires {new Date(session.expires_at).toLocaleString()}</small
						>{#if session.operations_verified}<small>Operations verification active</small>{/if}
					</div>
				</article>{/each}
		</div>
		<button class="button button-secondary" onclick={revokeOthers} disabled={busy || sessions.length < 2}
			>{busy ? 'Signing out…' : 'Sign out other sessions'}</button
		>{:else if !message}<p class="page-intro">Loading active sessions…</p>{/if}
	<div class="notice notice-info">
		You sign in with a code sent to your email, so whoever controls that inbox controls your account. Keep it secure.
	</div>
</section>
