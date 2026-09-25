<script lang="ts">
	import '../styles.css';
	import Header from '$lib/components/Header.svelte';
	import Footer from '$lib/components/Footer.svelte';
	import { page } from '$app/state';
	let { children } = $props();
</script>
<a class="skip-link" href={page.url.pathname.startsWith('/app') ? '#workspace-content' : '#content'}>Skip to content</a>
<Header />
<main id="content" tabindex="-1">{@render children()}</main>
<Footer />
