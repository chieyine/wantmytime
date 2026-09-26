<script lang="ts">
	import { api } from '$lib/api';
	let email = $state('');
	let message = $state('');
	let busy = $state(false);
	let next = $state('/app');
	$effect(() => {
		if (typeof window !== 'undefined') {
			const candidate = new URLSearchParams(window.location.search).get('next');
			if (candidate) {
				const target = new URL(candidate, window.location.origin);
				if (target.origin === window.location.origin) next = target.pathname + target.search + target.hash;
			}
		}
	});
	async function submit() {
		busy = true;
		message = '';
		try {
			const result = await api<{ challenge_id: string }>('/api/v1/auth/challenges', {
				method: 'POST',
				body: JSON.stringify({ email, purpose: 'login' })
			});
			window.location.href = `/verify?purpose=login&challenge=${encodeURIComponent(result.challenge_id)}&next=${encodeURIComponent(next)}`;
		} catch (error) {
			message = error instanceof Error ? error.message : 'Email verification could not be started.';
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>Log in — WantMyTime</title></svelte:head>
<section class="form-page">
	<p class="eyebrow">Welcome back</p>
	<h1 class="page-heading">Log in.</h1>
	<p class="page-intro">Enter your email and we’ll send you a code. No password to remember.</p>
	<form
		class="setup-form"
		onsubmit={(e) => {
			e.preventDefault();
			submit();
		}}
	>
		<label
			>Email<input
				class="field"
				type="email"
				bind:value={email}
				autocomplete="email"
				required
				placeholder="you@example.com"
			/></label
		><button class="button" type="submit" disabled={busy}
			>{busy ? 'Sending your code…' : 'Send my code'} <span aria-hidden="true">↗</span></button
		>{#if message}<p class="notice notice-warning" role="alert">{message}</p>{/if}
	</form>
	<p class="form-note">
		New here? <a class="text-link" href="/claim">Get your link</a>.
	</p>
</section>
