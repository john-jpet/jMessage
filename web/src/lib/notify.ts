import { readSettings } from "../state/settings";

let audioCtx: AudioContext | null = null;

/** blip plays a short synthesized notification tone (no audio asset). */
export function blip() {
  try {
    audioCtx ??= new AudioContext();
    const t = audioCtx.currentTime;
    const osc = audioCtx.createOscillator();
    const gain = audioCtx.createGain();
    osc.type = "sine";
    osc.frequency.setValueAtTime(880, t);
    osc.frequency.exponentialRampToValueAtTime(660, t + 0.08);
    gain.gain.setValueAtTime(0.001, t);
    gain.gain.exponentialRampToValueAtTime(0.08, t + 0.01);
    gain.gain.exponentialRampToValueAtTime(0.001, t + 0.12);
    osc.connect(gain).connect(audioCtx.destination);
    osc.start(t);
    osc.stop(t + 0.13);
  } catch {
    /* audio unavailable (autoplay policy, no device) — fine */
  }
}

/** ensureNotifyPermission requests desktop-notification permission
 *  (call from a user gesture, e.g. the settings toggle). */
export async function ensureNotifyPermission(): Promise<boolean> {
  if (!("Notification" in window)) return false;
  if (Notification.permission === "granted") return true;
  if (Notification.permission === "denied") return false;
  return (await Notification.requestPermission()) === "granted";
}

/** notifyMessage surfaces an incoming message per the user's settings:
 *  optional sound, optional desktop notification with preview gating. */
export function notifyMessage(sender: string, body: string) {
  const s = readSettings();
  if (s.sound) blip();
  if (
    s.desktopNotifications &&
    "Notification" in window &&
    Notification.permission === "granted"
  ) {
    new Notification(sender, {
      body: s.showPreviews ? body : "New message",
      silent: true, // our blip covers sound; avoid double audio
    });
  }
}
