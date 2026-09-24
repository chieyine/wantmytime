<script lang="ts">
	import { api } from '$lib/api';
	let email = $state(''); let message = $state(''); let busy = $state(false);
	const next = typeof window !== 'undefined' ? new URLSearchParams(window.location.search).get('next') : null;
	async function submit() {
		busy=true; message='';
		try {
			const challenge=await api<{challenge_id:string}>('/api/v1/access/challenges',{method:'POST',body:JSON.stringify({email})});
			window.location.href=`/verify?purpose=access&challenge=${encodeURIComponent(challenge.challenge_id)}${next?`&next=${encodeURIComponent(next)}`:''}`;
		} catch(e) { message=e instanceof Error?e.message:'A verification code could not be sent.'; }
		finally { busy=false; }
	}
</script>
<svelte:head><title>Access your bookings — WantMyTime</title></svelte:head>
<section class="form-page"><a class="back-link" href="/">← WantMyTime</a><p class="eyebrow">Secure booking access</p><h1 class="page-heading">Open your bookings.</h1><p class="page-intro">Enter the email used when booking. We’ll send a code if email delivery is available. Your bookings appear only after you verify it.</p>
	<form class="setup-form" onsubmit={(e)=>{e.preventDefault();submit()}}><label>Email<input class="field" type="email" bind:value={email} autocomplete="email" required /></label>
		{#if message}<p class="notice notice-warning" role="alert">{message}</p>{/if}
		<button class="button" disabled={busy||!email.trim()}>{busy?'Sending code…':'Send verification code'}</button>
	</form>
</section>
