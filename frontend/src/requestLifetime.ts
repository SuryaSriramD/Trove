import { useEffect, useRef } from "react";

// A continuation may publish or issue another read only in the render identity
// that started it. A key change invalidates synchronously, before effects run.
export function useRequestLifetime(key: string): () => boolean {
  const latest = useRef(key);
  latest.current = key;
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  return () => mounted.current && latest.current === key;
}
