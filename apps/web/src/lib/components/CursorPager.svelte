<script lang="ts">
	let { cursor, busy = false, onNext }: { cursor: string | null; busy?: boolean; onNext: () => void } = $props();
</script>

{#if cursor}
	<div class="form-actions"><button class="button button-secondary" type="button" onclick={onNext} disabled={busy}>{busy ? 'Loading…' : 'Load older records'}</button></div>
{/if}
