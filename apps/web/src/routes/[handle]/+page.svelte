<script lang="ts">
	import PublicPersonPage from '$lib/components/PublicPersonPage.svelte';
	import { onMount } from 'svelte';
	import { recordProductEvent } from '$lib/analytics';
	let { data } = $props();
	let person = $derived(data.person);
	let publicUrl = $derived(`${data.publicOrigin.replace(/\/$/, '')}/${person.handle}`);
	let firstName = $derived(person.name.split(' ')[0]);
	let previewUrl = $derived(`${data.publicOrigin.replace(/\/$/, '')}/og/${encodeURIComponent(person.handle)}/${person.preview_version}.png`);
	onMount(()=>{void recordProductEvent('public_link_viewed',person.handle)});
</script>
<svelte:head>
	<title>{person.name} — WantMyTime</title>
	<meta name="description" content={`A little time with ${person.name}.`} />
	<meta name="robots" content="noindex, nofollow" />
	<link rel="canonical" href={publicUrl} />
	<meta property="og:type" content="profile" />
	<meta property="og:title" content={`${person.name} — WantMyTime`} />
	<meta property="og:description" content={`A little time with ${firstName}.`} />
	<meta property="og:image" content={previewUrl} />
	<meta property="og:url" content={publicUrl} />
	<meta name="twitter:card" content="summary" />
</svelte:head>
<div class="public-person-route"><PublicPersonPage {person} /></div>
