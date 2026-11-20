import { focusManager, type QueryClient } from "@tanstack/react-query";

/**
 * Slows the console's polling while nobody is using it.
 *
 * Every poll is a TanStack Query `refetchInterval`, which only fetches while the focus manager says
 * the page is focused (unless a query sets `refetchIntervalInBackground`). By default that means
 * "the tab is visible", so a console left open on a screen asks for its counts every few seconds
 * forever. Here the page also counts as unfocused after IDLE_MS without a key, pointer, wheel or
 * touch event: the polls pause, and every IDLE_REFRESH_MS the polled queries refresh once, so the
 * screen stays roughly current. The first input after that counts as a refocus: stale queries
 * refetch at once (`refetchOnWindowFocus`) and the polls carry on at their own rates. Server-sent
 * events (`/api/live`) and terminal sockets are pushed by the server and aren't affected.
 */
export const IDLE_MS = 2 * 60_000;
export const IDLE_REFRESH_MS = 60_000;

const ACTIVITY = ["pointermove", "pointerdown", "keydown", "wheel", "touchstart", "scroll"] as const;

export function watchIdle(client: QueryClient): () => void {
  let lastInput = Date.now();
  let idle = false;
  let refresh: number | undefined;
  let report: ((focused?: boolean) => void) | undefined;

  const visible = () => document.visibilityState === "visible";
  const publish = () => report?.(visible() && !idle);

  // Only the polled queries a mounted page watches: a refresh shouldn't refetch every page of an
  // infinite query or anything that waits for an invalidation.
  const refreshPolled = () =>
    void client.refetchQueries({
      type: "active",
      predicate: (query) => query.observers.some((observer) => Boolean(observer.options.refetchInterval)),
    });

  const goIdle = () => {
    if (idle) return;
    idle = true;
    publish();
    refresh = window.setInterval(() => {
      if (visible()) refreshPolled();
    }, IDLE_REFRESH_MS);
  };

  const wake = () => {
    lastInput = Date.now();
    if (!idle) return;
    idle = false;
    window.clearInterval(refresh);
    refresh = undefined;
    publish();
  };

  // A check every few seconds rather than a timer reset on each pointer move, which fires constantly.
  const check = window.setInterval(() => {
    if (!idle && Date.now() - lastInput >= IDLE_MS) goIdle();
  }, 5_000);

  const onVisibility = () => {
    // Coming back to the tab is using it.
    if (visible()) wake();
    publish();
  };

  focusManager.setEventListener((setFocused) => {
    report = setFocused;
    publish();
    document.addEventListener("visibilitychange", onVisibility);
    return () => document.removeEventListener("visibilitychange", onVisibility);
  });
  for (const name of ACTIVITY) window.addEventListener(name, wake, { capture: true, passive: true });

  return () => {
    window.clearInterval(check);
    window.clearInterval(refresh);
    for (const name of ACTIVITY) window.removeEventListener(name, wake, { capture: true });
    report = undefined;
    focusManager.setEventListener((setFocused) => {
      const listener = () => setFocused();
      document.addEventListener("visibilitychange", listener);
      return () => document.removeEventListener("visibilitychange", listener);
    });
  };
}
