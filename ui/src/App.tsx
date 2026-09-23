import { useState } from "react";

import styles from "./App.module.css";
import { AppsView } from "./components/apps/AppsView";
import { Banner } from "./components/common/Banner";
import { StatusBar } from "./components/common/StatusBar";
import { Panel } from "./components/panel/Panel";
import { DiagnosticsView } from "./components/views/DiagnosticsView";
import { PlaceholderView } from "./components/views/PlaceholderView";
import { cx } from "./lib/cx";
import { CapabilitiesProvider } from "./state/CapabilitiesContext";
import { ConfigProvider, useConfig } from "./state/ConfigContext";
import { ConnectionProvider } from "./state/ConnectionContext";

const VIEWS = ["panel", "apps", "profiles", "diagnostics"] as const;
type View = (typeof VIEWS)[number];

const VIEW_LABELS: Record<View, string> = {
  panel: "Panel",
  apps: "Apps & Groups",
  profiles: "Profiles",
  diagnostics: "Diagnostics",
};

/** ConfigChangedBanner is the whole of M07's save-semantics design
 * beyond per-dialog apply: a non-blocking notice when the daemon's
 * config changed underneath this session (a physical long-press, a
 * SIGHUP reload, or another window's save), offering a reload -- never
 * an automatic one, which could discard an open editor's in-progress
 * typing. */
function ConfigChangedBanner() {
  const { changedExternally, reload, dismissChanged } = useConfig();
  if (!changedExternally) return null;
  return (
    <Banner
      actions={[
        { label: "Reload", onClick: () => void reload() },
        { label: "Dismiss", onClick: dismissChanged },
      ]}
    >
      knobd changed the configuration while this window was open.
    </Banner>
  );
}

function ViewContent({ view }: { view: View }) {
  switch (view) {
    case "panel":
      return <Panel />;
    case "apps":
      return <AppsView />;
    case "profiles":
      return <PlaceholderView title="Profiles" note="Profile management lands in a later commit of this milestone." />;
    case "diagnostics":
      return <DiagnosticsView />;
  }
}

export function App() {
  const [view, setView] = useState<View>("panel");

  return (
    <ConnectionProvider>
      <ConfigProvider>
        <CapabilitiesProvider>
          <div className={styles.app}>
            <StatusBar />
            <ConfigChangedBanner />
            <nav className={styles.tabs}>
              {VIEWS.map((v) => (
                <button
                  key={v}
                  type="button"
                  className={cx(styles.tab, v === view && styles.tabActive)}
                  onClick={() => {
                    setView(v);
                  }}
                >
                  {VIEW_LABELS[v]}
                </button>
              ))}
            </nav>
            <div className={styles.content}>
              <ViewContent view={view} />
            </div>
          </div>
        </CapabilitiesProvider>
      </ConfigProvider>
    </ConnectionProvider>
  );
}
