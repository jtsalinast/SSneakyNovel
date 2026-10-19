<script>
  // Shared "Randomize" button — squircle dark pill with a 3D rolling dice
  // icon and expanding gradient label on hover (same family as
  // AiGenerateButton). The dice face tumbles like a roulette wheel while the
  // pointer is over it, and flips one more face + full roll on click.
  import { translate, uiLocale } from '../lib/i18n/index.js';
  import { onDestroy } from 'svelte';
  import { createEventDispatcher } from 'svelte';

  const dispatch = createEventDispatcher();

  export let busy = false;
  export let disabled = false;
  // Either an i18n key (labelKey) or a literal label (text) — text wins when set.
  export let labelKey = 'config.story.randomize';
  export let busyLabelKey = 'config.generating';
  export let text = '';
  export let title = '';

  // Reactive labels. Keep the store subscription on its own `$:` line: a
  // comma-joined statement like `$: $uiLocale, labelText = ...` only assigns
  // the FIRST expression in Svelte, which would leave `labelText` undefined
  // and crash every page rendering this button (blank main area).
  $: void $uiLocale;
  $: labelText = text || translate(labelKey);
  $: busyText = translate(busyLabelKey);

  let hovered = false;
  let clicked = false;
  let rollRotation = 0;
  let clickTimer = null;
  let flipTimer = null;
  // Stable unique id for the SVG gradient (Math.random() in markup would
  // re-evaluate on every re-render and break url(#...) references).
  const gradId = 'auroraDice_' + Math.random().toString(36).slice(2, 8);

  // Pip layouts for each dice face (viewBox 0 0 24 24).
  const FACE_PIPS = {
    1: [{ cx: 12, cy: 12 }],
    2: [{ cx: 8.5, cy: 8.5 }, { cx: 15.5, cy: 15.5 }],
    3: [{ cx: 8.5, cy: 8.5 }, { cx: 12, cy: 12 }, { cx: 15.5, cy: 15.5 }],
    4: [{ cx: 8.5, cy: 8.5 }, { cx: 15.5, cy: 8.5 }, { cx: 8.5, cy: 15.5 }, { cx: 15.5, cy: 15.5 }],
    5: [{ cx: 8.5, cy: 8.5 }, { cx: 15.5, cy: 8.5 }, { cx: 12, cy: 12 }, { cx: 8.5, cy: 15.5 }, { cx: 15.5, cy: 15.5 }],
    6: [{ cx: 8.5, cy: 7 }, { cx: 15.5, cy: 7 }, { cx: 8.5, cy: 12 }, { cx: 15.5, cy: 12 }, { cx: 8.5, cy: 17 }, { cx: 15.5, cy: 17 }],
  };

  let currentFace = 5;
  let flipSeq = 0; // bumped on every face change to restart the CSS flip

  function renderFace(faceNum) {
    currentFace = faceNum;
    flipSeq += 1;
  }

  function getRandomFace(exclude) {
    let next;
    do {
      next = Math.floor(Math.random() * 6) + 1;
    } while (next === exclude);
    return next;
  }

  function onMouseEnter() {
    hovered = true;
    clearInterval(flipTimer);
    // Continuously roll faces like a casino roulette wheel (ultra-fast 80ms).
    flipTimer = setInterval(() => renderFace(getRandomFace(currentFace)), 80);
  }

  function onMouseLeave() {
    hovered = false;
    clearInterval(flipTimer);
    flipTimer = null;
  }

  // NOTE: this handler is bound with on:click|stopPropagation below. In Svelte
  // an on:click HANDLER (with or without the modifier) REPLACES the default
  // component event forwarding — without the explicit createEventDispatcher
  // here, parents using `on:click` would NEVER fire and the button would look
  // completely dead (this was the randomize regression). stopPropagation also
  // prevents the native DOM click from bubbling up to any ancestor handler.
  function onClick(e) {
    dispatch('click', e);
    if (disabled || busy) return;
    clicked = true;
    rollRotation += 360;
    renderFace(getRandomFace(currentFace));
    clearTimeout(clickTimer);
    clickTimer = setTimeout(() => (clicked = false), 600);
  }

  $: pips = FACE_PIPS[currentFace] || FACE_PIPS[1];

  onDestroy(() => {
    clearInterval(flipTimer);
    clearTimeout(clickTimer);
  });
</script>

<button
  type="button"
  class="randomize-btn"
  class:hovered
  class:clicked
  class:is-busy={busy}
  aria-label={labelText}
  {title}
  disabled={disabled || busy}
  on:mouseenter={onMouseEnter}
  on:mouseleave={onMouseLeave}
  on:click|stopPropagation={onClick}
>
  <div class="magic-border-glow"></div>

  <div class="btn-content">
    <span class="dice-container">
      {#if busy}
        <span class="loading loading-spinner loading-xs"></span>
      {:else}
        <svg class="dice-svg" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
          <defs>
            <linearGradient id={gradId} x1="0%" y1="0%" x2="100%" y2="100%">
              <stop offset="0%" stop-color="#a7f3d0" />
              <stop offset="50%" stop-color="#a5f3fc" />
              <stop offset="100%" stop-color="#c4b5fd" />
            </linearGradient>
          </defs>
          <g class="dice-entire-body" style="transform: rotate({rollRotation}deg)">
            <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
            <g class="face-flip" key={flipSeq}>
              <rect x="4" y="4" width="16" height="16" rx="4" stroke={`url(#${gradId})`} stroke-width="2" fill="none" />
              {#each pips as pip}
                <circle cx={pip.cx} cy={pip.cy} r="1.35" fill={`url(#${gradId})`} />
              {/each}
            </g>
          </g>
        </svg>
      {/if}
    </span>

    <span class="label">{busy ? busyText : labelText}</span>
  </div>
</button>

<style>
  .randomize-btn {
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

  .randomize-btn.hovered:not(:disabled) {
    width: 138px;
    border-color: rgba(255, 255, 255, 0.22);
    background: #14171d;
    box-shadow: 0 8px 24px -4px rgba(0, 0, 0, 0.25),
                inset 0 1px 0 rgba(255, 255, 255, 0.2);
  }

  .randomize-btn:active:not(:disabled) {
    transform: scale(0.94);
  }

  .randomize-btn:disabled {
    opacity: 0.55;
    cursor: not-allowed;
  }

  .randomize-btn.is-busy {
    width: 138px;
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
    background: linear-gradient(135deg, #a7f3d0 0%, #a5f3fc 50%, #c4b5fd 100%);
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    background-clip: text;
  }

  .randomize-btn.hovered .label,
  .randomize-btn.is-busy .label {
    opacity: 1;
    transform: translateX(0);
  }

  .dice-container {
    width: 20px;
    height: 20px;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    color: #a5f3fc;
  }

  .dice-svg {
    width: 20px;
    height: 20px;
    overflow: visible;
  }

  /* Unified dice body: identical dynamic floating motion in IDLE and HOVER,
     plus the click roll rotation via inline transform. */
  .dice-entire-body {
    transform-origin: 12px 12px;
    transition: transform 0.4s cubic-bezier(0.175, 0.885, 0.32, 1.275);
    filter: drop-shadow(0 0 4px rgba(167, 243, 208, 0.4));
    animation: hover3dTumble 1.8s ease-in-out infinite alternate;
  }

  /* Rapid flip when the face updates (re-mounted via key={flipSeq}). */
  .face-flip {
    transform-origin: 12px 12px;
    animation: flip3d 0.08s ease-out;
  }

  @keyframes flip3d {
    0% {
      transform: scale(1) rotate(0deg);
    }
    50% {
      transform: scale(0.7) rotateX(45deg) rotateY(20deg);
      opacity: 0.85;
    }
    100% {
      transform: scale(1) rotate(0deg);
      opacity: 1;
    }
  }

  @keyframes hover3dTumble {
    0% {
      transform: translateY(-1px) rotate(-10deg) scale(1.05);
    }
    100% {
      transform: translateY(-2px) rotate(14deg) scale(1.1);
    }
  }

  .magic-border-glow {
    position: absolute;
    inset: -1px;
    border-radius: 12px;
    border: 1.5px solid transparent;
    background: linear-gradient(135deg, #a7f3d0, #a5f3fc, #c4b5fd) border-box;
    -webkit-mask: linear-gradient(#fff 0 0) padding-box, linear-gradient(#fff 0 0);
    -webkit-mask-composite: destination-out;
    mask-composite: exclude;
    opacity: 0;
    pointer-events: none;
    transition: opacity 0.3s ease;
  }

  .randomize-btn.clicked .magic-border-glow {
    opacity: 1;
    animation: borderFlash 0.6s cubic-bezier(0.16, 1, 0.3, 1) forwards;
  }

  @keyframes borderFlash {
    0% { opacity: 1; transform: scale(0.98); }
    40% { opacity: 0.9; transform: scale(1.03); }
    100% { opacity: 0; transform: scale(1.06); }
  }
</style>
