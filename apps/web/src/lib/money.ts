export function formatNaira(minor: number | string | bigint | null | undefined): string {
  let value: bigint;
  try {
    value = typeof minor === 'bigint' ? minor : typeof minor === 'string' ? BigInt(minor.trim() || '0') : BigInt(Math.trunc(Number.isFinite(minor as number) ? (minor as number) : 0));
  } catch {
    value = 0n;
  }
  const negative = value < 0n;
  const abs = negative ? -value : value;
  const naira = abs / 100n;
  const kobo = abs % 100n;
  return `${negative ? '-' : ''}₦${new Intl.NumberFormat('en-NG').format(naira)}${kobo ? `.${kobo.toString().padStart(2, '0')}` : ''}`;
}

export function parseNairaToMinor(input: string): bigint | null {
  const clean = input.replace(/[₦,\s]/g, '');
  if (!/^\d+(?:\.\d{1,2})?$/.test(clean)) return null;
  const [whole, decimal = ''] = clean.split('.');
  return BigInt(whole) * 100n + BigInt(decimal.padEnd(2, '0'));
}

// Mirrors the API's quote rule: round(base_30 * minutes / 30), halves rounded up.
export function priceForDuration(base30Minor: number | string, minutes: number): number {
	const raw = typeof base30Minor === 'string' ? Number(base30Minor) : base30Minor;
	if (!Number.isFinite(raw) || raw < 0) return 0;
	const base = BigInt(Math.trunc(raw));
	return Number((base * BigInt(minutes) + 15n) / 30n);
}
