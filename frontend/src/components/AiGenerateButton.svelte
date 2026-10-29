<script>
  // Shared "AI Generate" button — squircle dark pill with animated aurora
  // sparks, expanding gradient label on hover (Revolut/Notion style).
  // Used by every section-level Generate button (Config, Outline pre-plan).
  import { translate, uiLocale } from '../lib/i18n/index.js';
  import { createEventDispatcher } from 'svelte';

  const dispatch = createEventDispatcher();

  export let busy = false;
  export let disabled = false;
  // Either a i18n key (labelKey) or a literal label (text) — text wins when set.
  export let labelKey = 'common.generate';
  export let busyLabelKey = 'config.generating';
  export let text = '';
  export let title = '';
  // When true the button fades in only while the pointer is over its card
  // (.group/card) or grouped section (.group/hover-section), matching the
  // existing .hover-target mechanism in Config.svelte. The host must live
  // inside one of those groups.
  export let hoverReveal = false;
  // Expanded width. Buttons anchored to the right edge (flex justify-end)
  // keep that edge fixed and grow leftwards, so longer labels need more room.
  export let expandedWidth = 132;
  // "Send to AI" variant: white paper-plane icon + white label on the same
  // black pill (no aurora sparkles).
  export let plane = false;

  // Reactive labels. Keep the store subscription on its own `$:` line: a
  // comma-joined statement like `$: $uiLocale, labelText = ...` only assigns
  // the FIRST expression in Svelte, which left `labelText` undefined and
  // crashed every page rendering this button (blank main area).
  $: void $uiLocale;
  $: labelText = text || translate(labelKey);
  $: busyText = translate(busyLabelKey);

  let hovered = false;
  let clicked = false;
  let spin = 0;
  let clickTimer = null;
  // Stable unique id for the SVG gradient (Math.random() in markup would
  // re-evaluate on every re-render and break url(#...) references).
  const gradId = 'auroraGlow_' + Math.random().toString(36).slice(2, 8);

  function onMouseEnter() {
    hovered = true;
  }
  function onMouseLeave() {
    hovered = false;
  }
</script>

<!-- The internal on:click handler below dispatches the component "click"
     event explicitly (a bound handler disables Svelte's automatic forwarding),
     so parents' `on:click` fire exactly once. -->
<button
  type="button"
  class="ai-btn"
  style="--ai-expanded-width: {expandedWidth}px"
  class:hovered
  class:clicked
  class:is-busy={busy}
  class:is-plane={plane}
  class:hover-target={hoverReveal}
  aria-label={labelText}
  {title}
  disabled={disabled || busy}
  on:mouseenter={onMouseEnter}
  on:mouseleave={onMouseLeave}
  on:click|stopPropagation={(e) => {
    // CRITICAL: binding an on:click HANDLER here (instead of leaving the bare
    // forwarded `on:click` that Svelte re-emits automatically) DISABLES the
    // default component event forwarding. Without this explicit dispatch,
    // every parent's `on:click={genClick(...)}` silently never fires and all
    // Generate buttons look dead. Keep in sync with RandomizeButton.svelte.
    dispatch('click', e);
    if (disabled || busy) return;
    clicked = true;
    spin += 360;
    clearTimeout(clickTimer);
    clickTimer = setTimeout(() => (clicked = false), 700);
  }}
>
  <div class="magic-border-glow"></div>

  <div class="btn-content">
    <span class="sparks-container">
      {#if busy}
        <span class="loading loading-spinner loading-xs"></span>
      {:else if plane}
        <svg class="plane-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M22 2 11 13" /><path d="M22 2 15 22 11 13 2 9 22 2Z" />
        </svg>
      {:else}
        <!-- svelte-ignore a11y-click-events-have-key-events -->
        <svg class="spark-svg" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
          <defs>
            <linearGradient id={gradId} x1="0%" y1="0%" x2="100%" y2="100%">
              <stop offset="0%" stop-color="#fbcfe8" />
              <stop offset="50%" stop-color="#c4b5fd" />
              <stop offset="100%" stop-color="#bae6fd" />
            </linearGradient>
          </defs>
          <g class="orbit-wrapper" style="transform: rotate({spin}deg)">
            <path class="diamond-top-right" d="M18 3C18 4.65685 19.3431 6 21 6C19.3431 6 18 7.34315 18 9C18 7.34315 16.6569 6 15 6C16.6569 6 18 4.65685 18 3Z" fill={`url(#${gradId})`} />
            <path class="diamond-bottom-left" d="M6 15C6 16.1046 5.10457 17 4 17C5.10457 17 6 17.8954 6 19C6 17.8954 6.89543 17 8 17C6.89543 17 6 16.1046 6 15Z" fill={`url(#${gradId})`} />
          </g>
          <g class="center-wrapper" style="transform: rotate({-spin * 1.5}deg)">
            <path class="diamond-center" d="M12 3C12 7.97056 7.97056 12 3 12C7.97056 12 12 16.0294 12 21C12 16.0294 16.0294 12 21 12C16.0294 12 12 7.97056 12 3Z" fill={`url(#${gradId})`} />
          </g>
        </svg>
      {/if}
    </span>

    <span class="label">{busy ? busyText : labelText}</span>
  </div>
</button>

<style>
  .ai-btn {
    position: relative;
    display: inline-flex;
    align-items: center;
    justify-content: flex-start;
    height: 42px;
    width: 42px;
    padding: 0;
    background: #0f1115;
    border: 1px solid rgba(255, 255, 255, 0.12);
    border-radius: 12px;
    color: #ffffff;
    cursor: pointer;
    outline: none;
    overflow: hidden;
    transition: width 0.45s cubic-bezier(0.16, 1, 0.3, 1),
                border-color 0.25s ease,
                box-shadow 0.35s ease,
                background-color 0.25s ease,
                transform 0.1s cubic-bezier(0.16, 1, 0.3, 1);
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.12),
                0 8px 16px -4px rgba(0, 0, 0, 0.18),
                inset 0 1px 0 rgba(255, 255, 255, 0.1);
    flex-shrink: 0;
  }

  .ai-btn.hovered:not(:disabled) {
    width: var(--ai-expanded-width, 132px);
    border-color: rgba(255, 255, 255, 0.22);
    background: #14171d;
    box-shadow: 0 8px 24px -4px rgba(0, 0, 0, 0.25),
                inset 0 1px 0 rgba(255, 255, 255, 0.2);
  }

  .ai-btn:active:not(:disabled) {
    transform: scale(0.96);
  }

  .ai-btn:disabled {
    opacity: 0.55;
    cursor: not-allowed;
  }

  .ai-btn.is-busy {
    width: var(--ai-expanded-width, 132px);
  }

  /* Inverted "Submit to AI" variant: white pill with grey icon and label. */
  .ai-btn.is-plane {
    background: #ffffff;
    border-color: rgba(0, 0, 0, 0.12);
    color: #4b5563;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.06);
  }

  .ai-btn.is-plane.hovered:not(:disabled) {
    background: #f9fafb;
    border-color: rgba(0, 0, 0, 0.2);
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.08);
  }

  .btn-content {
    position: relative;
    z-index: 2;
    display: flex;
    align-items: center;
    width: 100%;
    height: 100%;
    padding-left: 10px;
  }

  .label {
    position: absolute;
    left: 38px;
    font-weight: 600;
    font-size: 13.5px;
    letter-spacing: -0.1px;
    white-space: nowrap;
    opacity: 0;
    transform: translateX(6px);
    transition: opacity 0.22s ease 0.08s, transform 0.35s cubic-bezier(0.16, 1, 0.3, 1) 0.08s;
    user-select: none;
    background: linear-gradient(135deg, #fbcfe8 0%, #c4b5fd 50%, #bae6fd 100%);
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    background-clip: text;
  }

  .ai-btn.hovered .label,
  .ai-btn.is-busy .label {
    opacity: 1;
    transform: translateX(0);
  }

  .sparks-container {
    width: 20px;
    height: 20px;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    color: #c4b5fd;
  }

  .spark-svg {
    width: 20px;
    height: 20px;
    overflow: visible;
  }

  .plane-icon {
    width: 20px;
    height: 20px;
    color: #4b5563;
  }

  .ai-btn.is-plane .label {
    background: none;
    -webkit-text-fill-color: #4b5563;
    color: #4b5563;
  }

  .orbit-wrapper,
  .center-wrapper {
    transform-origin: 12px 12px;
    transition: transform 0.75s cubic-bezier(0.16, 1, 0.3, 1);
  }

  .diamond-center {
    transform-origin: 12px 12px;
    animation: pulseCenter 3s ease-in-out infinite alternate;
    filter: drop-shadow(0 0 4px rgba(224, 231, 255, 0.45));
  }

  .diamond-top-right {
    transform-origin: 12px 12px;
    animation: floatTopRight 2.2s ease-in-out infinite alternate;
    filter: drop-shadow(0 0 3px rgba(224, 231, 255, 0.4));
  }

  .diamond-bottom-left {
    transform-origin: 12px 12px;
    animation: floatBottomLeft 2.5s ease-in-out infinite alternate;
    filter: drop-shadow(0 0 3px rgba(224, 231, 255, 0.4));
  }

  .ai-btn.hovered .diamond-center {
    animation: spinCenterHover 3.5s linear infinite;
  }

  .ai-btn.hovered .diamond-top-right {
    animation: orbitTopHover 1.8s ease-in-out infinite alternate;
  }

  .ai-btn.hovered .diamond-bottom-left {
    animation: orbitBottomHover 2s ease-in-out infinite alternate;
  }

  @keyframes pulseCenter {
    0% { transform: scale(0.92) rotate(0deg); }
    100% { transform: scale(1.05) rotate(6deg); }
  }

  @keyframes floatTopRight {
    0% { transform: translate(0, 0) scale(0.95); }
    100% { transform: translate(0.8px, -1.2px) scale(1.1); }
  }

  @keyframes floatBottomLeft {
    0% { transform: translate(0, 0) scale(1.05); }
    100% { transform: translate(-0.8px, 1.2px) scale(0.9); }
  }

  @keyframes spinCenterHover {
    from { transform: rotate(0deg) scale(1.05); }
    to { transform: rotate(-360deg) scale(1.05); }
  }

  @keyframes orbitTopHover {
    0% { transform: translate(0, 0) rotate(0deg); }
    100% { transform: translate(1.5px, -2px) rotate(30deg); }
  }

  @keyframes orbitBottomHover {
    0% { transform: translate(0, 0) rotate(0deg); }
    100% { transform: translate(-2px, 1.5px) rotate(-30deg); }
  }

  .magic-border-glow {
    position: absolute;
    inset: -1px;
    border-radius: 12px;
    border: 1.5px solid transparent;
    background: linear-gradient(135deg, #f472b6, #a78bfa, #38bdf8) border-box;
    -webkit-mask: linear-gradient(#fff 0 0) padding-box, linear-gradient(#fff 0 0);
    -webkit-mask-composite: destination-out;
    mask-composite: exclude;
    opacity: 0;
    pointer-events: none;
    transition: opacity 0.3s ease;
  }

  .ai-btn.clicked .magic-border-glow {
    opacity: 1;
    animation: borderFlash 0.7s cubic-bezier(0.16, 1, 0.3, 1) forwards;
  }

  @keyframes borderFlash {
    0% { opacity: 1; transform: scale(0.99); }
    50% { opacity: 0.9; transform: scale(1.02); }
    100% { opacity: 0; transform: scale(1.04); }
  }

  /* Hover-reveal mode (hoverReveal={true}): mirrors the .hover-target rules in
     Config.svelte so the button stays invisible until the pointer is over its
     card (.group/card) or grouped section (.group/hover-section). Global and
     idempotent with the page's own rules; focus-within keeps it keyboard-usable,
     and a busy button is always visible so progress isn't hidden. */
  :global(.hover-target) {
    opacity: 0;
    pointer-events: none;
    transform: translateY(-2px);
    transition: opacity 260ms cubic-bezier(0.16, 1, 0.3, 1),
                transform 260ms cubic-bezier(0.16, 1, 0.3, 1);
  }
  :global(:is(.group\/card, .group\/hover-section):has(.group\/hover-section) .hover-target) {
    opacity: 0;
    pointer-events: none;
    transform: translateY(-2px);
    transition: opacity 260ms cubic-bezier(0.16, 1, 0.3, 1),
                transform 260ms cubic-bezier(0.16, 1, 0.3, 1);
  }
  :global(.group\/hover-section:hover .hover-target),
  :global(.group\/hover-section:focus-within .hover-target) {
    opacity: 1;
    pointer-events: auto;
    transform: translateY(0);
  }
  :global(:is(.group\/card, .group\/hover-section):not(:has(.group\/hover-section)):hover .hover-target),
  :global(:is(.group\/card, .group\/hover-section):not(:has(.group\/hover-section)):focus-within .hover-target) {
    opacity: 1;
    pointer-events: auto;
    transform: translateY(0);
  }
  :global(.hover-target.is-busy) {
    opacity: 1;
    pointer-events: auto;
    transform: translateY(0);
  }
</style>
