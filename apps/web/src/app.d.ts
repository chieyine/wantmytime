// See https://svelte.dev/docs/kit/types#app.d.ts
declare global {
	namespace App {
		interface Locals {
			/** Ties this request to gateway, API and error-report records. */
			requestId: string;
		}
		interface Error {
			message: string;
			/** Shown to the person so support can find the matching logs. */
			reference?: string;
		}
	}
}

export {};
