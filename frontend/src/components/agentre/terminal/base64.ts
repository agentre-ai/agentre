// Terminal stdout arrives base64-encoded: raw PTY bytes survive the JSON event
// bridge that way (a UTF-8 string would have multibyte sequences split across
// chunks mangled to U+FFFD; see terminal_svc.pump). Decode to bytes and hand
// them to xterm, which reassembles split multibyte sequences across writes.
export function base64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  return bytes;
}
