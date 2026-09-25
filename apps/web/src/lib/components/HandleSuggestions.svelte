<script lang="ts">
	// Free links to pick with one tap, shown under a link field.
	let {
		suggestions,
		current = '',
		label = 'Also free',
		onpick
	} = $props<{ suggestions: string[]; current?: string; label?: string; onpick: (handle: string) => void }>();
	let shown = $derived(suggestions.filter((s: string) => s !== current.trim().toLowerCase()));
</script>

{#if shown.length}
	<div class="handle-suggestions" role="group" aria-label="Available links">
		<span>{label}:</span>
		{#each shown as handle (handle)}
			<button type="button" onclick={() => onpick(handle)}>{handle}</button>
		{/each}
	</div>
{/if}
