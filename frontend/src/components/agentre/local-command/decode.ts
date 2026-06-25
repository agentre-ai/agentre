import { base64ToBytes } from "../terminal/base64";

export function makeStreamDecoder() {
  const dec = new TextDecoder();
  return (b64: string): string => dec.decode(base64ToBytes(b64), { stream: true });
}
