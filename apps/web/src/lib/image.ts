/**
 * Optimizes an avatar image before upload by resizing it to a maximum dimension
 * (default 512px) using an offscreen canvas. This prevents oversized payload errors
 * and dramatically speeds up mobile uploads.
 */
export async function optimizeImageForAvatar(file: File, maxDim = 512): Promise<File> {
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

				// If already within max dimensions and comfortably small (< 300 KB), no resize needed
				if (width <= maxDim && height <= maxDim && file.size <= 300 * 1024) {
					resolve(file);
					return;
				}

				// Calculate proportional dimensions
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

				// Export as JPEG at 0.88 quality for compact, high-fidelity avatar
				canvas.toBlob(
					(blob) => {
						if (!blob) {
							resolve(file);
							return;
						}
						const baseName = file.name.replace(/\.[^/.]+$/, '');
						const optimizedFile = new File([blob], `${baseName}.jpg`, {
							type: 'image/jpeg',
							lastModified: Date.now()
						});
						resolve(optimizedFile);
					},
					'image/jpeg',
					0.88
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
