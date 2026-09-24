import { useCallback, useEffect, useState } from "react";

import {
  isBridgeError,
  spotifyDevices,
  spotifyPlaylists,
  type SpotifyDevice,
  type SpotifyPlaylist,
} from "../api/client";

/** useSpotifyPlaylists/useSpotifyDevices fetch GET /spotify/playlists|
 * devices on demand -- the same per-picker, fetch-on-mount shape
 * useAudioGraph uses, not a continuously-updated feed (these lists
 * change far less often than the audio graph, and are only ever looked
 * at while a binding editor field is open). Both requests 409 (not yet
 * authorized) before the OAuth flow completes; that shows up here as
 * `error`, and the picker falls back to free text the same way
 * TargetField does for an unplugged device. */
function useSpotifyList<T>(fetch: () => Promise<T[]>): {
  items: T[] | undefined;
  loading: boolean;
  error: string | undefined;
} {
  const [items, setItems] = useState<T[] | undefined>(undefined);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | undefined>(undefined);

  const reload = useCallback(async () => {
    setLoading(true);
    try {
      const next = await fetch();
      setItems(next);
      setError(undefined);
    } catch (err) {
      setError(isBridgeError(err) ? err.message : String(err));
    } finally {
      setLoading(false);
    }
    // fetch is a stable module-level function reference at each call
    // site (spotifyPlaylists/spotifyDevices below), so omitting it from
    // deps is intentional, mirroring useAudioGraph's reload.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void reload();
  }, [reload]);

  return { items, loading, error };
}

export function useSpotifyPlaylists(): {
  playlists: SpotifyPlaylist[] | undefined;
  loading: boolean;
  error: string | undefined;
} {
  const { items, loading, error } = useSpotifyList(spotifyPlaylists);
  return { playlists: items, loading, error };
}

export function useSpotifyDevices(): {
  devices: SpotifyDevice[] | undefined;
  loading: boolean;
  error: string | undefined;
} {
  const { items, loading, error } = useSpotifyList(spotifyDevices);
  return { devices: items, loading, error };
}
