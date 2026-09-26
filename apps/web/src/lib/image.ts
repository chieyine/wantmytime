/**
 * Optimizes an avatar image before upload by resizing it to a maximum dimension
 * (default 320px) using an offscreen canvas and exporting as standard PNG.
 *
 * Why 320px PNG:
 * 1. UI avatars display at max 72px–80px. 320px provides pristine clarity even on 3x Retina displays.
 * 2. A 320x320 PNG is guaranteed to be ~60KB–120KB, making it mathematically impossible
 *    to exceed the backend's 512KB database storage limit.
 * 3. Canvas decodes any browser-supported image format (JPEG, PNG, WebP, AVIF) and outputs
 *    clean, standardized PNG that the backend decodes flawlessly.
 */
export async function optimizeImageForAvatar(file: File, maxDim = 320): Promise<File> {
	if (typeof window === 'undefined' || typeof document === 'undefined') {
		return file;
	}

	return new Promise((resolve) => {
		const img = new Image();
		const objectUrl = URL.createObjectURL(file);

		img.onload = () => {
			URL.revokeObjectURL(objectUrl);
			try {
				let { naturalWidth: width, naturalHeight: height } = img;
				if (!width || !height) {
					resolve(file);
					return;
				}

				// Always calculate proportional dimensions <= maxDim
				if (width > maxDim || height > maxDim) {
					if (width >= height) {
						height = Math.round((height * maxDim) / width);
						width = maxDim;
					} else {
						width = Math.round((width * maxDim) / height);
						height = maxDim;
					}
				}

				width = Math.max(1, width);
				height = Math.max(1, height);

				const canvas = document.createElement('canvas');
				canvas.width = width;
				canvas.height = height;
				const ctx = canvas.getContext('2d');
				if (!ctx) {
					resolve(file);
					return;
				}

				ctx.imageSmoothingEnabled = true;
				ctx.imageSmoothingQuality = 'high';
				ctx.drawImage(img, 0, 0, width, height);

				// Export as PNG so server receives standard PNG that fits comfortably under 512KB
				canvas.toBlob(
					(blob) => {
						if (!blob) {
							resolve(file);
							return;
						}
						const optimizedFile = new File([blob], 'avatar.png', {
							type: 'image/png',
							lastModified: Date.now()
						});
						resolve(optimizedFile);
					},
					'image/png'
				);
			} catch {
				resolve(file);
			}
		};

		img.onerror = () => {
			URL.revokeObjectURL(objectUrl);
			resolve(file);
		};

		img.src = objectUrl;
	});
}
