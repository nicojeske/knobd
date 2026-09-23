import { useCallback, useEffect, useState } from "react";

import { getAudio, isBridgeError, type AudioGraph } from "../api/client";

export interface AudioGraphInfo {
  graph: AudioGraph | undefined;
  loading: boolean;
  error: string | undefined;
  reload: () => Promise<void>;
}

/** useAudioGraph fetches GET /audio on demand -- a plain hook, not a
 * context, since it's opened per-picker rather than kept live for the
 * whole app (unlike ConnectionContext's push-driven GET /state): the
 * target picker and the app/group manager both need "what's running
 * right now," fetched fresh each time they're opened, not a
 * continuously-updated feed. */
export function useAudioGraph(): AudioGraphInfo {
  const [graph, setGraph] = useState<AudioGraph | undefined>(undefined);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | undefined>(undefined);

  const reload = useCallback(async () => {
    setLoading(true);
    try {
      const next = await getAudio();
      setGraph(next);
      setError(undefined);
    } catch (err) {
      setError(isBridgeError(err) ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    // Fetch-on-mount, same as ConfigContext: reload's first synchronous
    // statement re-affirms the initial `useState(true)` value, which
    // React bails out of re-rendering for (Object.is-equal), so this
    // isn't the cascading-render pattern the rule guards against.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void reload();
  }, [reload]);

  return { graph, loading, error, reload };
}
