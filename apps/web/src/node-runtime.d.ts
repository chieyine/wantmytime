declare module 'node:buffer' {
	export type Buffer = any;
	export const Buffer: any;
}

declare module 'node:zlib' {
	export function deflateSync(data: Uint8Array): Uint8Array;
}
