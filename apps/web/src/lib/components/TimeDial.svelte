<script lang="ts">
	import { onMount } from 'svelte';
	let { duration = 30, compact = false } = $props<{ duration?: number; compact?: boolean }>();
	let now = $state(new Date());
	let frameTime = $state(Date.now());
	const ticks = Array.from({ length: 60 }, (_, index) => index);
	const circumference = 2 * Math.PI * 187;
	let hourAngle = $derived(((new Date(frameTime).getHours() % 12) + new Date(frameTime).getMinutes() / 60) * 30);
	let minuteAngle = $derived((new Date(frameTime).getMinutes() + new Date(frameTime).getSeconds() / 60) * 6);
	let secondAngle = $derived((new Date(frameTime).getSeconds() + (frameTime % 1000) / 1000) * 6);
	onMount(() => {
		let frame = 0;
		let last = 0;
		const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
		if (reduceMotion) {
			const interval = window.setInterval(() => {
				frameTime = Date.now();
				now = new Date(frameTime);
			}, 1000);
			return () => window.clearInterval(interval);
		}
		const tick = (time: number) => {
			if (time - last > 32) {
				frameTime = Date.now();
				now = new Date(frameTime);
				last = time;
			}
			frame = requestAnimationFrame(tick);
		};
		frame = requestAnimationFrame(tick);
		return () => cancelAnimationFrame(frame);
	});
	function endpoint(angle: number, radius: number) {
		const radians = ((angle - 90) * Math.PI) / 180;
		return { x: 300 + Math.cos(radians) * radius, y: 300 + Math.sin(radians) * radius };
	}
</script>

<div class:compact class="time-dial" aria-label={`Live clock. ${duration} minute conversation selected.`} role="img">
	<svg viewBox="0 0 600 600" aria-hidden="true" focusable="false">
		<circle class="dial-outer" cx="300" cy="300" r="278" />
		<circle class="dial-inner" cx="300" cy="300" r="187" />
		<circle class="dial-selection-track" cx="300" cy="300" r="187" />
		<circle
			class="dial-selection"
			cx="300"
			cy="300"
			r="187"
			stroke-dasharray={`${(circumference * duration) / 60} ${circumference}`}
			style={`--reveal-length: ${(circumference * duration) / 60}px`}
			transform="rotate(-90 300 300)"
		/>
		{#each ticks as tick (tick)}
			{@const start = endpoint(tick * 6, tick % 5 === 0 ? 246 : 261)}
			{@const end = endpoint(tick * 6, 276)}
			<line
				class:major={tick % 5 === 0}
				class="dial-tick"
				style={`--tick-index: ${tick}`}
				x1={start.x}
				y1={start.y}
				x2={end.x}
				y2={end.y}
			/>
		{/each}
		<text class="dial-number" x="300" y="89" text-anchor="middle">12</text>
		<text class="dial-number" x="513" y="309" text-anchor="middle">03</text>
		<text class="dial-number" x="300" y="529" text-anchor="middle">06</text>
		<text class="dial-number" x="87" y="309" text-anchor="middle">09</text>
		<line class="dial-hour" x1="300" y1="300" x2={endpoint(hourAngle, 111).x} y2={endpoint(hourAngle, 111).y} />
		<line class="dial-minute" x1="300" y1="300" x2={endpoint(minuteAngle, 154).x} y2={endpoint(minuteAngle, 154).y} />
		<line class="dial-second" x1="300" y1="300" x2={endpoint(secondAngle, 166).x} y2={endpoint(secondAngle, 166).y} />
		<circle class="dial-pin" cx="300" cy="300" r="7" />
	</svg>
	<div class="time-dial-caption">
		<span>YOUR LOCAL TIME</span><span
			>{String(now.getHours()).padStart(2, '0')}:{String(now.getMinutes()).padStart(2, '0')}:{String(
				now.getSeconds()
			).padStart(2, '0')}</span
		>
	</div>
</div>
