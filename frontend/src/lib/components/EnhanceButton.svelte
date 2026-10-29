<script>
  // Wand "Enhance" pill: appears inside a focused Novel parameters field and
  // asks the LLM to rewrite that field's draft. The parent owns the dispatch
  // (chat fast path) and the busy flag.
  import { createEventDispatcher } from 'svelte';

  export let busy = false;
  export let label = 'Enhance';
  export let inline = false;

  const dispatch = createEventDispatcher();
  const gradientId = 'wandAurora' + Math.random().toString(36).slice(2, 9);
  let clicked = false;

  function onClick() {
    if (busy) return;
    clicked = true;
    setTimeout(() => { clicked = false; }, 700);
    dispatch('click');
  }
</script>

<button type="button" class="appeal-enhance-btn" class:clicked class:busy class:inline
  disabled={busy} aria-label={label} title={label} on:click={onClick}>
  <span class="magic-border-glow"></span>
  <span class="btn-content">
    <span class="icon-container">
      {#if busy}
        <span class="loading loading-spinner loading-xs"></span>
      {:else}
        <svg class="wand-svg" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
          <defs>
            <linearGradient id={gradientId} x1="0%" y1="0%" x2="100%" y2="100%">
              <stop offset="0%" class="aurora-stop1" />
              <stop offset="50%" class="aurora-stop2" />
              <stop offset="100%" class="aurora-stop3" />
            </linearGradient>
          </defs>
          <g class="wand-body">
            <path d="M4.5 19.5L12.5 11.5" stroke={`url(#${gradientId})`} stroke-width="2.2" stroke-linecap="round"/>
            <path d="M12.5 11.5L15 9" stroke={`url(#${gradientId})`} stroke-width="2.2" stroke-linecap="round"/>
            <path d="M12 12L13 11" stroke={`url(#${gradientId})`} stroke-width="1.6" stroke-linecap="round"/>
            <path class="star-tip-linear" d="M17 1.5L18.4 4.8L21.8 5.2L19.2 7.5L20 10.8L17 9.1L14 10.8L14.8 7.5L12.2 5.2L15.6 4.8L17 1.5Z" stroke={`url(#${gradientId})`} stroke-width="1.5" stroke-linejoin="round" fill="#FAFAFA"/>
            <path class="particle-filled-1" d="M5 4.5L5.5 5.8L6.8 6.2L5.8 7.1L6.1 8.3L5 7.6L3.9 8.3L4.2 7.1L3.2 6.2L4.5 5.8L5 4.5Z" fill={`url(#${gradientId})`}/>
            <path class="particle-filled-2" d="M20 13.5L20.4 14.5L21.5 14.8L20.7 15.5L20.9 16.5L20 16L19.1 16.5L19.3 15.5L18.5 14.8L19.6 14.5L20 13.5Z" fill={`url(#${gradientId})`}/>
            <circle cx="10" cy="4" r="1" fill={`url(#${gradientId})`} />
          </g>
        </svg>
      {/if}
    </span>
    <span class="enhance-label">{label}</span>
  </span>
</button>

<style>
  /* Wand "Enhance" button — collapsed 32px pill that expands to the
     left-to-right label on hover, with the neon pink / fuchsia / electric
     indigo / cyan aurora gradient shared by icon and text, floating
     particles, a spinning tip star on hover and the magic border flash. */
  .appeal-enhance-btn {
    position: absolute;
    top: 8px;
    right: 8px;
    z-index: 5;
    display: inline-flex;
    align-items: center;
    justify-content: flex-start;
    height: 32px;
    width: 32px;
    padding: 0;
    background: #ffffff;
    border: 1px solid rgba(0, 0, 0, 0.12);
    border-radius: 16px;
    color: #111827;
    cursor: pointer;
    outline: none;
    overflow: visible;
    transition: width 0.4s cubic-bezier(0.16, 1, 0.3, 1),
                border-color 0.25s ease,
                box-shadow 0.35s ease,
                transform 0.1s cubic-bezier(0.16, 1, 0.3, 1);
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06),
                0 8px 16px -4px rgba(0, 0, 0, 0.08);
    animation: enhancePopIn 0.2s cubic-bezier(0.16, 1, 0.3, 1) both;
  }
  @keyframes enhancePopIn {
    from { opacity: 0; transform: scale(0.8); }
    to   { opacity: 1; transform: scale(1); }
  }
  /* Single-line inputs are only 32px tall: center the pill vertically instead
     of using the textarea's top offset (which would hang below the field). */
  .appeal-enhance-btn.inline {
    top: calc(50% - 16px);
  }
  .appeal-enhance-btn:hover:not(:disabled) {
    width: 108px;
    border-color: rgba(0, 0, 0, 0.2);
    background: #ffffff;
    box-shadow: 0 8px 24px -4px rgba(0, 0, 0, 0.12),
                0 2px 8px rgba(0, 0, 0, 0.04);
  }
  .appeal-enhance-btn:active:not(:disabled) { transform: scale(0.96); }
  .appeal-enhance-btn:disabled { opacity: 0.85; cursor: wait; }
  .appeal-enhance-btn .btn-content {
    position: relative;
    z-index: 2;
    display: flex;
    align-items: center;
    justify-content: flex-start;
    width: 100%;
    height: 100%;
    padding-left: 8px;
  }
  .enhance-label {
    position: absolute;
    left: 31px;
    font-weight: 600;
    font-size: 11.5px;
    letter-spacing: -0.1px;
    white-space: nowrap;
    opacity: 0;
    visibility: hidden;
    transform: translateX(-8px);
    transition: opacity 0.12s ease 0s, transform 0.15s ease 0s, visibility 0s linear 0.15s;
    user-select: none;
    color: #8b5cf6;
    background: linear-gradient(135deg, #ec4899 0%, #8b5cf6 50%, #06b6d4 100%);
    background-size: 200% 200%;
    -webkit-background-clip: text;
    background-clip: text;
    -webkit-text-fill-color: transparent;
    animation: vibrantAuroraShift 4s ease infinite alternate;
  }
  .appeal-enhance-btn:hover:not(:disabled) .enhance-label {
    opacity: 1;
    visibility: visible;
    transform: translateX(0);
    transition: opacity 0.25s ease 0.12s,
                transform 0.3s cubic-bezier(0.16, 1, 0.3, 1) 0.12s,
                visibility 0s linear 0s;
  }
  .appeal-enhance-btn .icon-container {
    width: 16px;
    height: 16px;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }
  .appeal-enhance-btn .wand-svg {
    width: 16px;
    height: 16px;
    overflow: visible;
  }
  .aurora-stop1 { stop-color: #ec4899; animation: vibrantStop1 4s ease-in-out infinite alternate; }
  .aurora-stop2 { stop-color: #8b5cf6; animation: vibrantStop2 4s ease-in-out infinite alternate; }
  .aurora-stop3 { stop-color: #06b6d4; animation: vibrantStop3 4s ease-in-out infinite alternate; }
  @keyframes vibrantAuroraShift {
    0%   { background-position: 0% 50%; }
    100% { background-position: 100% 50%; }
  }
  @keyframes vibrantStop1 {
    0%   { stop-color: #ec4899; }
    50%  { stop-color: #f43f5e; }
    100% { stop-color: #8b5cf6; }
  }
  @keyframes vibrantStop2 {
    0%   { stop-color: #8b5cf6; }
    50%  { stop-color: #6366f1; }
    100% { stop-color: #06b6d4; }
  }
  @keyframes vibrantStop3 {
    0%   { stop-color: #06b6d4; }
    50%  { stop-color: #3b82f6; }
    100% { stop-color: #ec4899; }
  }
  .star-tip-linear {
    transform-origin: 17px 6.15px;
    transition: transform 0.3s ease;
    animation: starFloatTip 2.5s ease-in-out infinite alternate;
  }
  .particle-filled-1 {
    transform-origin: 5px 6px;
    transition: transform 0.3s ease;
    animation: particleFloat1 2.2s ease-in-out infinite alternate;
  }
  .particle-filled-2 {
    transform-origin: 20px 14px;
    transition: transform 0.3s ease;
    animation: particleFloat2 2.8s ease-in-out infinite alternate;
  }
  .appeal-enhance-btn:hover:not(:disabled) .star-tip-linear {
    animation: starContinuousRotate 2.5s linear infinite;
  }
  .appeal-enhance-btn:hover:not(:disabled) .particle-filled-1 {
    animation: particlePulse1 0.75s ease-in-out infinite alternate;
  }
  .appeal-enhance-btn:hover:not(:disabled) .particle-filled-2 {
    animation: particlePulse2 0.9s ease-in-out 0.15s infinite alternate;
  }
  @keyframes starFloatTip {
    0%   { transform: scale(0.95) rotate(-5deg); }
    100% { transform: scale(1.1) rotate(12deg); }
  }
  @keyframes starContinuousRotate {
    0%   { transform: rotate(0deg) scale(1.1); }
    100% { transform: rotate(360deg) scale(1.1); }
  }
  @keyframes particleFloat1 {
    0%   { transform: scale(0.85) translate(0, 0); }
    100% { transform: scale(1.15) translate(-0.5px, -1px); }
  }
  @keyframes particleFloat2 {
    0%   { transform: scale(0.8) translate(0, 0); }
    100% { transform: scale(1.2) translate(0.5px, 0.8px); }
  }
  @keyframes particlePulse1 {
    0%   { transform: scale(0.8) rotate(0deg); }
    100% { transform: scale(1.4) rotate(20deg); }
  }
  @keyframes particlePulse2 {
    0%   { transform: scale(0.75) rotate(0deg); }
    100% { transform: scale(1.35) rotate(-25deg); }
  }
  .wand-body {
    transform-origin: 5px 19px;
    transition: transform 0.25s cubic-bezier(0.16, 1, 0.3, 1);
  }
  .appeal-enhance-btn:hover:not(:disabled) .wand-body {
    transform: rotate(-6deg) translateY(-0.5px);
  }
  .magic-border-glow {
    position: absolute;
    inset: -1px;
    border-radius: 16px;
    border: 1.5px solid transparent;
    background: linear-gradient(135deg, #ec4899, #8b5cf6, #06b6d4) border-box;
    -webkit-mask: linear-gradient(#fff 0 0) padding-box, linear-gradient(#fff 0 0);
    -webkit-mask-composite: destination-out;
    mask: linear-gradient(#fff 0 0) padding-box, linear-gradient(#fff 0 0);
    mask-composite: exclude;
    opacity: 0;
    pointer-events: none;
    transition: opacity 0.3s ease;
  }
  .appeal-enhance-btn.clicked .magic-border-glow {
    opacity: 1;
    animation: borderFlash 0.7s cubic-bezier(0.16, 1, 0.3, 1) forwards;
  }
  @keyframes borderFlash {
    0%   { opacity: 1; transform: scale(0.98); }
    50%  { opacity: 0.9; transform: scale(1.03); }
    100% { opacity: 0; transform: scale(1.05); }
  }
  .appeal-enhance-btn.busy .loading-spinner,
  .appeal-enhance-btn .icon-container .loading { color: #8b5cf6; }
</style>
