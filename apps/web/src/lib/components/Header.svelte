<script lang="ts">
	import { brand } from '$lib/brand';
</script>
<header class="site-header">
	<a class="wordmark" href="/" aria-label={`${brand.name} home`}>{brand.name}<span class="wordmark-period">.</span></a>
	<nav aria-label="Main navigation"><a href="/#how">How it works</a><a href="/pricing">Pricing</a></nav>
	<div class="header-actions"><a class="login-link" href="/login">Log in</a><a class="button button-small" href="/claim">Get your link</a></div>
</header>
