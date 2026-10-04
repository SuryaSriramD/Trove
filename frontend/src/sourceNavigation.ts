import type { D } from "./api";

// Locate only canonical identities already returned by the citation service.
// Source revisions are validated by the caller before showing current content.
export function focusSourceLocation(location: D.Location): () => void {
  let frame = 0;
  let attempts = 0;
  let disposed = false;
  let audio: HTMLAudioElement | null = null;
  let seek: (() => void) | undefined;
  let focused: HTMLElement | null = null;
  const visit = () => {
    if (disposed) return;
    if (location.blockId) window.dispatchEvent(new CustomEvent("trove:reveal-block", {detail: {blockId: location.blockId}}));
    const block = location.blockId
      ? document.querySelector<HTMLElement>(`[data-block-id="${CSS.escape(location.blockId)}"]`)
      : document.querySelector<HTMLElement>(".page-heading");
    const targetAudio = location.transcript
      ? document.querySelector<HTMLAudioElement>(`audio[data-meeting-id="${CSS.escape(location.transcript.meetingId)}"]`)
      : null;
    if ((!block || (location.transcript && !targetAudio)) && attempts++ < 120) {
      frame = requestAnimationFrame(visit);
      return;
    }
    if (block) {
      for (let parent: HTMLElement | null = block; parent; parent = parent.parentElement) parent.hidden = false;
      block.scrollIntoView({ behavior: "smooth", block: "center" });
      block.classList.add("source-highlight");
      focused = block;
    }
    if (targetAudio && location.transcript) {
      audio = targetAudio;
      const seconds = location.transcript.startMs / 1000;
      seek = () => { if (audio && !disposed) audio.currentTime = seconds; };
      if (audio.readyState >= 1) seek();
      else audio.addEventListener("loadedmetadata", seek, { once: true });
      // Seeking is explicit navigation; playback still requires the user.
    }
  };
  frame = requestAnimationFrame(visit);
  return () => {
    disposed = true;
    cancelAnimationFrame(frame);
    if (audio && seek) audio.removeEventListener("loadedmetadata", seek);
    focused?.classList.remove("source-highlight");
  };
}
