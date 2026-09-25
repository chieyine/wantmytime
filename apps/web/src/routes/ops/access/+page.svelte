<script lang="ts">
	import { api } from '$lib/api';
	let code = $state('');
	let message = $state('');
	let busy = $state(false);
	async function verify() {
		busy = true;
		message = '';
		try {
			await api('/api/v1/ops/session', { method: 'POST', body: JSON.stringify({ code: code.replace(/\s/g, '') }) });
			location.href = '/ops';
		} catch (e) {
			message = e instanceof Error ? e.message : 'Operations verification failed.';
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>Operations access — WantMyTime</title></svelte:head>
<section class="form-page app-page">
	<a class="back-link" href="/ops">← Operations</a>
	<p class="eyebrow">Restricted access</p>
	<h1 class="page-heading">Verify your operator account.</h1>
	<p class="page-intro">
		Sign in with your verified email first, then enter the current code from your registered authenticator.
	</p>
	<p><a class="text-link" href="/login?next=%2Fops%2Faccess">Sign in to your account ↗</a></p>
	<div class="setup-form">
		<label
			>Authenticator code<input
				class="field"
				inputmode="numeric"
				autocomplete="one-time-code"
				maxlength="6"
				bind:value={code}
			/></label
		><button class="button" onclick={verify} disabled={busy || code.length !== 6}>Verify and continue</button
		>{#if message}<p class="notice notice-warning" aria-live="polite">{message}</p>{/if}
	</div>
</section>
