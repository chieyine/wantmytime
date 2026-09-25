<script lang="ts">
	import { marketingWording } from '$lib/marketing';
	let { checked = $bindable(true) } = $props<{ checked?: boolean }>();
</script>

<label class="check-line marketing-consent"><input type="checkbox" bind:checked /> {marketingWording}</label>
