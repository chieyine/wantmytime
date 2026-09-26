<script lang="ts">
	import '../styles.css';
	import Header from '$lib/components/Header.svelte';
	import Footer from '$lib/components/Footer.svelte';
	import { page, navigating } from '$app/state';
	import { onNavigate } from '$app/navigation';
	let { children } = $props();

	onNavigate((navigation) => {
		if (!document.startViewTransition) return;
		return new Promise((resolve) => {
			document.startViewTransition(async () => {
				resolve();
				await navigation.complete;
			});
		});
	});
</script>

{#if navigating.to}
	<div class="nav-loading-bar" aria-hidden="true"></div>
{/if}
<a class="skip-link" href={page.url.pathname.startsWith('/app') ? '#workspace-content' : '#content'}>Skip to content</a>
<Header />
<main id="content" tabindex="-1">{@render children()}</main>
<Footer />
