const FLASH_DISPLAY_LIMIT = 80;

export function truncateFlashText(text: string): {
  display: string;
  full: string;
  truncated: boolean;
} {
  const full = text;
  const normalized = text.replace(/[\r\n\t]+/g, " ").replace(/ {2,}/g, " ");
  if (normalized.length <= FLASH_DISPLAY_LIMIT) {
    return { display: normalized, full, truncated: false };
  }
  return {
    display: normalized.slice(0, FLASH_DISPLAY_LIMIT) + "…",
    full,
    truncated: true,
  };
}
