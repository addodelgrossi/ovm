<script lang="ts">
  import { onMount } from 'svelte'
  import { BuildInfo } from '../wailsjs/go/main/App.js'

  type VersionInfo = {
    version: string
    commit: string
    date: string
  }

  let versionInfo: VersionInfo = {
    version: 'dev',
    commit: 'unknown',
    date: 'unknown'
  }

  onMount(async () => {
    try {
      versionInfo = await BuildInfo()
    } catch {
      // Standalone browser development does not expose Wails bindings.
    }
  })
</script>

<svelte:head>
  <title>ovm — ONVIF-compatible device manager</title>
</svelte:head>

<div class="app-shell">
  <aside class="sidebar" aria-label="Devices">
    <div class="titlebar-drag-region" aria-hidden="true"></div>

    <div class="brand-row">
      <div class="brand-mark" aria-hidden="true">
        <svg viewBox="0 0 32 32" role="img">
          <rect x="5" y="9" width="19" height="14" rx="4"></rect>
          <circle cx="14.5" cy="16" r="4"></circle>
          <path d="m24 13 4-2v10l-4-2z"></path>
          <path d="M9 6h11"></path>
        </svg>
      </div>
      <div>
        <p class="eyebrow">Device manager</p>
        <h1>ovm</h1>
      </div>
    </div>

    <div class="toolbar">
      <button
        type="button"
        class="primary-button"
        disabled
        title="Available in milestone M1"
      >
        <svg viewBox="0 0 20 20" aria-hidden="true">
          <path d="M10 4v12M4 10h12"></path>
        </svg>
        Scan network
      </button>
    </div>

    <div class="device-list-empty">
      <span class="status-ring" aria-hidden="true"></span>
      <p>No devices yet</p>
      <small>Discovery and manual setup arrive in the next milestone.</small>
    </div>

    <footer>
      <span>Version {versionInfo.version}</span>
      <span class="status-dot" aria-label="Foundation status: ready"></span>
    </footer>
  </aside>

  <main class="content">
    <header class="content-header">
      <div>
        <p class="eyebrow">Welcome</p>
        <h2>Connect your first camera</h2>
      </div>
      <span class="milestone-badge">Foundation</span>
    </header>

    <section class="welcome-card" aria-labelledby="getting-started-title">
      <div class="network-illustration" aria-hidden="true">
        <div class="camera-node">
          <svg viewBox="0 0 48 48">
            <rect x="7" y="13" width="27" height="22" rx="7"></rect>
            <circle cx="20" cy="24" r="7"></circle>
            <path d="m34 19 8-4v18l-8-4z"></path>
          </svg>
        </div>
        <span></span>
        <span></span>
        <span></span>
      </div>

      <div class="welcome-copy">
        <p class="eyebrow">Getting started</p>
        <h3 id="getting-started-title">
          Local camera setup, without guesswork
        </h3>
        <p>
          ovm will discover ONVIF-compatible cameras, surface safe stream URLs,
          and make configuration changes verifiable.
        </p>

        <ol>
          <li><span>1</span>Connect the Mac to the camera network.</li>
          <li>
            <span>2</span>Choose the wired interface and allow Local Network
            access.
          </li>
          <li><span>3</span>Scan or add the device endpoint manually.</li>
        </ol>
      </div>
    </section>

    <section class="principles-grid" aria-label="Project principles">
      <article>
        <span class="article-icon" aria-hidden="true">⌁</span>
        <h3>Local by design</h3>
        <p>No telemetry and no cloud dependency.</p>
      </article>
      <article>
        <span class="article-icon" aria-hidden="true">◇</span>
        <h3>Credentials protected</h3>
        <p>Passwords stay in the native OS secret store.</p>
      </article>
      <article>
        <span class="article-icon" aria-hidden="true">✓</span>
        <h3>Changes verified</h3>
        <p>Every supported mutation is read back.</p>
      </article>
    </section>
  </main>
</div>
