<script>
  import { onMount, tick } from 'svelte';
  import { api, apiFetch } from '../lib/api.js';
  import { config, progress, taskRunning, streamingContent, streamingChapterIdx, selectedChapter, autoConfirm, addToast, confirmModal } from '../lib/stores.js';
  import { navigate } from '../lib/router.js';
  import { t } from '../lib/i18n/index.js';
  import { countProseUnits } from '../lib/proseUnits.js';
  import TaskTokenBadge from '../components/TaskTokenBadge.svelte';
  import KnowledgePanel from '../components/KnowledgePanel.svelte';
  import InfoTip from '../lib/components/InfoTip.svelte';
  import ScenePlanEditor from '../components/ScenePlanEditor.svelte';
  import BestsellerCritiqueCard from '../components/BestsellerCritiqueCard.svelte';
  import { settings } from '../lib/stores.js';
  let showKnowledge = false;
  let knowledgeName = '', knowledgeDescription = '', knowledgeTags = '';
  let selectedSettings = [];
  let savingKnowledge = false;
  async function saveKnowledge(revise = false) {
    if (!knowledgeName.trim() || !knowledgeDescription.trim() || savingKnowledge) return;
    savingKnowledge = true;
    const target = ch?.num;
    try {
      const entry = await api('POST', '/api/worldview', {name: knowledgeName.trim(), description: knowledgeDescription.trim(), tags: knowledgeTags.trim(), category: 'knowledge'});
      settings.update(s => ({...s, worldview: [...(s?.worldview || []).filter(w => w.id !== entry.id), entry]}));
      knowledgeName = ''; knowledgeDescription = ''; knowledgeTags = '';
      if (ch?.num === target) selectedSettings = [...new Set([...selectedSettings, entry.id])];
      addToast($t('writing.knowledge.saved'), 'success');
      if (revise && ch?.num === target) await doRevise();
    } catch (e) { addToast(e.message, 'error'); }
    finally { savingKnowledge = false; }
  }
  let facts = [];
  let activeFact = null;
  let reviewFacts = []; // facts copy shown in the Review & accept tab
  let knowledgeExpanded = true;
  let returnRef = null;

  // —— Writing section tabs (below WritingMetaRow) ——————————————————
  // Three-step horizontal tab bar: Scene matrix / Chapter writing /
  // Review & accept. Each step owns one pane of the chapter workflow.
  let writingTab = 1;
  const writingTabDefs = [
    { num: 1, titleKey: 'writing.tab.sceneMatrix' },
    { num: 2, titleKey: 'writing.tab.chapterWriting' },
    { num: 3, titleKey: 'writing.tab.reviewAccept' },
  ];
  function setWritingTab(n) {
    if (n < 1 || n > 3) return;
    writingTab = n;
    // Entering Review & accept: keep the facts copy in sync with the panel
    // the user may have just interacted with, and refresh it from the API.
    if (n === 3) {
      reviewFacts = facts;
      if (ch?.num) loadReviewFacts(ch.num);
    }
  }
  let highlightedBlock = null;
  let selectedBlockId = null;
  $: factsByBlock = new Map(chapterBlocks.map(b => [b.id, facts.filter(f => (f.references || []).some(r => r.chapter === ch?.num && r.block_id === b.id))]));
  const blockFacts = id => factsByBlock.get(id) || [];
  function confirmAction(message, action) {
    confirmModal.set({ message, onConfirm: action });
  }
  function selectBlock(id) {
    if (editingBlockId != null || revisingBlockId != null || insertAfterId != null) return;
    highlightedBlock = null;
    selectedBlockId = selectedBlockId === id ? null : id;
  }
  async function jumpToFact(ref, returning = false) {
    if (editingBlockId != null || revisingBlockId != null || insertAfterId != null) {
      confirmAction($t('facts.discard'), () => { cancelBlockOps(); jumpToFact(ref, returning); });
      return;
    }
    const idx = chapters.findIndex(c => c.num === ref.chapter);
    if (idx < 0) { addToast($t('facts.stale'), 'warning'); return; }
    if (!returning && !returnRef) returnRef = {chapter: ch?.num, block_id: highlightedBlock || chapterBlocks[0]?.id};
    try {
      const full = await api('GET', '/api/chapters/' + ref.chapter);
      const block = (full.blocks || []).find(b => b.id === ref.block_id);
      if (!block || (ref.quote && block.text !== ref.quote)) { addToast($t('facts.stale'), 'warning'); return; }
      selectedChapter.set(idx);
      loadedNum = ref.chapter;
      applyChapter(ref.chapter, full);
      highlightedBlock = ref.block_id;
      await tick();
      document.getElementById('story-block-' + ref.block_id)?.scrollIntoView({block: 'center', behavior: 'smooth'});
      if (returning) returnRef = null;
    } catch(e) { addToast(e.message, 'error'); }
  }
  function factHeaders(confirmed) {
    return {'X-Content-Rev': loadedRev || '', 'X-Confirm-Fact-Impact': String(confirmed)};
  }

  const OUTLINE_FOCUS_KEY = 'showmethestory.outlineFocusChapter';

  onMount(async () => {
    try {
      const res = await api('GET', '/api/autoconfirm');
      autoConfirm.set(!!res.enabled);
    } catch (e) {}
    try {
      const sk = await api('GET', '/api/skills');
      hasPolishSkills = (sk || []).some(s => s.enabled && s.skill?.category === 'polish');
    } catch (e) {}
    try {
      // Pre-planning act cards live in their own endpoint; the act-critic row
      // needs them to offer per-act manual analysis.
      const pp = await api('GET', '/api/outline/preplan');
      preplanActs = (pp?.acts || []).map((a, i) => ({
        no: i + 1, name: a.name || '', start_ch: a.start_ch || 0, end_ch: a.end_ch || 0,
      }));
    } catch (e) {}
  });

  async function toggleAutoConfirm(e) {
    const enabled = e.target.checked;
    try {
      const res = await api('PUT', '/api/autoconfirm', { enabled });
      autoConfirm.set(!!res.enabled);
      addToast(res.enabled ? $t('writing.toasts.autoConfirmOn') : $t('writing.toasts.autoConfirmOff'), 'info');
    } catch (err) {
      e.target.checked = $autoConfirm;
      addToast(err.message, 'error');
    }
  }

  $: p = $progress;
  $: inWriting = p?.phase === 'writing';
  $: chapters = p?.chapters || [];
  $: projectChapters = chapters.filter(c => !c.inherited);
  $: total = projectChapters.length;
  $: accepted = projectChapters.filter(c => c.status === 'accepted').length;
  $: pct = total > 0 ? Math.round(accepted / total * 100) : 0;
  $: currentIdx = p?.current_chapter_index ?? 0;

  // Select current chapter by default
  $: if (inWriting && ($selectedChapter < 0 || $selectedChapter >= chapters.length)) {
    selectedChapter.set(Math.min(currentIdx, chapters.length - 1));
  }

  // In auto-confirm mode, follow the chapter being generated
  $: if ($autoConfirm && $streamingChapterIdx >= 0 && $streamingChapterIdx < chapters.length && $streamingChapterIdx !== $selectedChapter) {
    selectedChapter.set($streamingChapterIdx);
  }

  $: ch = $selectedChapter >= 0 && $selectedChapter < chapters.length ? chapters[$selectedChapter] : null;
  $: isCurrent = ch && currentIdx === $selectedChapter;
  $: isStreamingThis = $streamingChapterIdx === $selectedChapter && $streamingContent;

  // /api/progress does not carry prose; fetch selected chapter content on demand，content_rev refresh on change
  let chapterContent = '';
  let chapterBlocks = [];
  let loadedNum = -1;
  let loadedRev = null;
  $: if (ch) maybeLoadContent(ch);
  function applyChapter(num, full) {
    if (loadedNum !== num) return;
    chapterContent = full.content || '';
    chapterBlocks = full.blocks || [];
    loadedRev = full.content_rev || '';
  }
  // The Review & accept tab renders KnowledgePanel with its OWN facts
  // binding (so the prose chips in tab 2 never see it); refresh that copy
  // whenever the selection changes or the chapter content settles.
  $: reviewFactsKey = (ch?.num ?? 0) + ':' + (ch?.content_rev || '');
  $: if (ch) loadReviewFacts(ch.num);
  async function loadReviewFacts(num) {
    try {
      const data = await api('GET', '/api/knowledge?chapter=' + num);
      if (ch?.num === num) reviewFacts = data.facts || [];
    } catch (e) {}
  }
  // Force a reload of the selected chapter's payload (blocks, scene/trope
  // meta, content_rev) even when the prose hash is unchanged — used by the
  // "Actualizar interfaz" resync, which rewrites metadata but not text.
  async function reloadChapterFull() {
    if (!ch) return null;
    const num = ch.num;
    loadedNum = num;
    try {
      const full = await api('GET', '/api/chapters/' + num);
      applyChapter(num, full);
      return full;
    } catch (e) { addToast(e.message, 'error'); return null; }
  }
  async function maybeLoadContent(c) {
    const rev = c.content_rev || '';
    if (c.num === loadedNum && rev === loadedRev) return;
    if (c.num !== loadedNum) selectedBlockId = null;
    loadedNum = c.num;
    loadedRev = rev;
    hideIVPopup();
    ivLocator = null; ivLocatorNum = -1; // stale tiling must never tint the new chapter
    if (!rev) { chapterContent = ''; chapterBlocks = []; return; }
    try {
      const full = await api('GET', '/api/chapters/' + c.num);
      applyChapter(c.num, full);
      loadIVLocator(c.num); // refresh the subscene tiling used to confine critic marks
    } catch (e) {}
  }
  $: hasContent = !!(ch?.content_rev);

  // —— Block Block editing ——
  let editingBlockId = null;   // block being edited inline
  let editingText = '';
  let revisingBlockId = null;  // block whose AI revision feedback is being typed
  let blockFeedback = '';
  let insertAfterId = null;    // id of the block after which a new block is inserted (0 = start)
  let insertBeforeId = null;   // id of the block BEFORE which a new subscene block is inserted

  // —— Continuous-sheet editor (Word-like) ——————————————————————————
  // The chapter is one always-editable prose surface instead of a stack of
  // per-block cards. Between paragraphs we render almost-invisible chips at
  // every scene boundary and every trope-unit boundary; chip text is styled
  // `user-select:none` so double-clicking prose never selects it. Editing is
  // local until the sticky "Save" button (rendered OUTSIDE the reader card)
  // serializes the sheet to PUT /api/chapters/{num}/sheet, which re-syncs
  // blocks/scenes/tropes on the server.
  let sheetBlocks = [];        // live working copy: {id, text}
  let sheetDirty = false;
  let sheetSaving = false;
  let sheetEl = null;          // contenteditable root
  let sheetFocused = false;    // pointer inside the sheet → static Save
  let saveBtnVisible = true;   // reader still in viewport?
  let readerAbove = false;     // user scrolled PAST the top of the reader card

  $: if (chapterBlocks !== lastChapterBlocksRef) {
    lastChapterBlocksRef = chapterBlocks;
    sheetBlocks = chapterBlocks.map(b => ({ id: b.id, text: b.text }));
    sheetDirty = false;
  }
  let lastChapterBlocksRef = null;

  $: if (saveBtnSentinel && typeof IntersectionObserver !== 'undefined') {
    const io = new IntersectionObserver(([e]) => { saveBtnVisible = e.isIntersecting; }, { rootMargin: '0px 0px -8px 0px' });
    io.observe(saveBtnSentinel);
  }
  let saveBtnSentinel = null;
  // Track whether the ChapterReader card has been scrolled ABOVE the viewport:
  // if so the Save button floats fixed at the bottom-right; once the card is
  // back in view (or we reach its end) it settles into the normal flow again.
  function setReaderCard(node) {
    if (typeof IntersectionObserver === 'undefined') return {};
    const io = new IntersectionObserver(([e]) => {
      readerAbove = !e.isIntersecting && e.boundingClientRect.top < 0;
    }, { threshold: 0 });
    io.observe(node);
    return { destroy() { io.disconnect(); } };
  }

  // Block bookkeeping for chips: scene/trope membership comes from the
  // ORIGINAL chapterBlocks (keyed by id); paragraphs typed into the sheet
  // that don't map to a known block simply carry no chip.
  const blockMetaById = () => {
    const m = new Map();
    for (const b of chapterBlocks) m.set(b.id, b);
    return m;
  };

  // Chips shown BEFORE block i. Scenes and subscenes are NEVER merged into a
  // single chip: when both boundaries coincide we return a LIST where the
  // SCENE chip comes first (rendered on top) and its subscene chip second
  // (rendered below). A scene chip appears whenever the scene changes
  // (including at i===0 — the first scene's chip must always render); a
  // subscene chip appears whenever the trope/beat index changes inside the
  // same scene, or as the first-beat chip right under a scene chip.
  // Legacy tolerance: blocks synced before `scene_has_meta` existed leave the
  // field undefined; treat that as "has meta" instead of silently dropping
  // every scene chip. Only an explicit `false` (plan-less fallback scenes with
  // no real metadata) suppresses the scene chip (the subscene chip still
  // renders from trope bookkeeping alone).
  function markersBefore(blocks, i, meta) {
    if (!blocks || i >= blocks.length) return [];
    const cur = meta.get(blocks[i].id);
    if (!cur || cur.scene_pos == null || cur.scene_pos < 0) return [];
    const prev = i > 0 ? meta.get(blocks[i - 1].id) : null;
    const out = [];
    // —— SCENE chip (top row) ——————————————————————————————————————————————
    const ps = prev && prev.scene_pos != null ? prev.scene_pos : -1;
    if (ps !== cur.scene_pos && cur.scene_has_meta !== false) {
      // Scene chips carry a tooltip with the scene's description. Prefer the
      // block-synced meta; fall back to the chapter's accepted scene plan so
      // the tooltip works even when sync metadata lacks title/description.
      let sTitle = cur.scene_title || '';
      let sDesc = cur.scene_description || '';
      if (!sDesc || !sTitle) {
        const planned = ((ch?.scene_plan?.scenes && ch.scene_plan.scenes.length ? ch.scene_plan.scenes : planByChapter[ch?.num]) || [])[cur.scene_pos];
        if (planned) {
          if (!sTitle) sTitle = planned.title || '';
          if (!sDesc) sDesc = planned.description || planned.purpose || '';
        }
      }
      out.push({ kind: 'scene', pos: cur.scene_pos + 1, title: sTitle, desc: sDesc });
    }
    // —— SUBSCENE chip (bottom row) —————————————————————————————————————————
    // Any change of trope index between consecutive blocks marks a new beat.
    // The backend tiles units 1-based (trope_index = ui+1), so after a
    // re-tile the previous block legitimately has a HIGHER index; requiring
    // `ct > pt` used to swallow every chip but the first. Any difference
    // (ct !== pt) is a boundary. When a scene boundary just fired, the FIRST
    // beat of the new scene also gets its chip (ct === 1) — that is the
    // "subscene 1 chip under the scene chip" the sheet must always show.
    const pt = prev && prev.trope_index != null ? prev.trope_index : 0;
    const ct = cur.trope_index != null ? cur.trope_index : 0;
    const sceneBoundary = ps !== cur.scene_pos;
    const isSubsceneChip = (ct !== pt && (cur.trope_count || 0) >= 1 && (prev?.trope_count ?? 1) >= 1)
      || (sceneBoundary && ct === 1 && (cur.trope_count || 0) >= 1);
    if (isSubsceneChip) {
      out.push({ kind: 'trope', pos: cur.scene_pos + 1, label: cur.trope_label || '', idx: ct, count: cur.trope_count || 0 });
    }
    return out;
  }
  // (markerTip was removed together with the emergent tooltips on the sheet
  // chips — hovering a chip now reveals the "add subscene before" action.)
  // firstUnitIssuesOfScene is kept for the block-level trope dots; the scene
  // chip itself no longer renders issue dots (chips stay clean: label only).

  // Serialize the contenteditable sheet back into {id,text} paragraphs.
  // Each top-level child carries data-bid (chip rows keep the bid of the
  // paragraph they precede; unmarked rows are user-created paragraphs given
  // negative ids, which the server treats as inserts during its re-sync).
  function serializeSheet() {
    if (!sheetEl) return;
    const out = [];
    for (const el of sheetEl.children) {
      const raw = el.getAttribute('data-bid');
      const text = (el.innerText || '').replace(/\u00a0/g, ' ').replace(/\s+\n/g, '\n').trim();
      if (!text) continue;
      if (raw == null) { out.push({ id: -(out.length + 1), text }); continue; }
      const id = Number(raw);
      if (id < 0) continue; // chip row — not editable prose
      const last = out[out.length - 1];
      if (last && last.id === id) last.text += '\n\n' + text; // split block piece
      else out.push({ id, text });
    }
    const changed = out.length !== sheetBlocks.length || out.some((b, i) => !sheetBlocks[i] || sheetBlocks[i].id !== b.id || sheetBlocks[i].text !== b.text);
    if (changed) { sheetBlocks = out; sheetDirty = true; }
  }

  // Svelte action for the continuous sheet surface: keeps the caret sane while
  // typing (we never re-render from state on input — the DOM is the source of
  // truth until Save) and dismisses the floating selection menu when the user
  // clicks elsewhere or collapses the selection.
  function sheetEditable(node) {
    const onDocMouseUp = (e) => { if (!node.contains(e.target)) hideSelMenu(); };
    const onDocSelection = () => {
      const sel = window.getSelection();
      if (!sel || sel.isCollapsed) hideSelMenu();
    };
    const onKeyDown = (e) => {
      // Ctrl/Cmd+S triggers the sticky Save button without leaving the page.
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
        e.preventDefault();
        saveSheet();
      }
    };
    document.addEventListener('mouseup', onDocMouseUp);
    document.addEventListener('selectionchange', onDocSelection);
    node.addEventListener('keydown', onKeyDown);
    return {
      destroy() {
        document.removeEventListener('mouseup', onDocMouseUp);
        document.removeEventListener('selectionchange', onDocSelection);
        node.removeEventListener('keydown', onKeyDown);
      },
    };
  }

  async function saveSheet(confirmed = false) {
    if (!ch || !sheetDirty || sheetSaving) return;
    serializeSheet();
    if (!sheetBlocks.length) return;
    try {
      const full = await api('PUT', `/api/chapters/${ch.num}/sheet`, { blocks: sheetBlocks }, factHeaders(confirmed === true));
      applyChapter(ch.num, full);
      sheetDirty = false;
      addToast($t('writing.sheet.saved'), 'success');
    } catch (e) {
      if (e.message && e.message.includes('fact_impact_confirm') && confirmed !== true) {
        confirmAction($t('facts.confirm'), () => saveSheet(true));
      } else {
        addToast(e.message, 'error');
      }
    }
  }

  // —— Floating selection menu over the sheet ————————————————————————
  // Replaces the old per-block toolbar: appears when the user selects text
  // inside the sheet; actions target the block(s) containing the selection.
  let selMenu = null; // {x,y,text,blockIds[]}
  function checkSheetSelection() {
    const sel = window.getSelection();
    if (!sel || sel.isCollapsed || sel.rangeCount === 0 || !sheetEl) { selMenu = null; return; }
    const text = sel.toString().trim();
    if (!text || text.length < 2) { selMenu = null; return; }
    const range = sel.getRangeAt(0);
    if (!sheetEl.contains(range.commonAncestorContainer)) { selMenu = null; return; }
    const rect = range.getBoundingClientRect();
    if (rect.width === 0 && rect.height === 0) { selMenu = null; return; }
    // Find the block ids whose DOM rows intersect the selection.
    const ids = [];
    for (const el of sheetEl.querySelectorAll('[data-bid]')) {
      const id = Number(el.getAttribute('data-bid'));
      if (id < 0) continue;
      const r = el.getBoundingClientRect();
      if (r.bottom >= rect.top && r.top <= rect.bottom) ids.push(id);
    }
    selMenu = { x: rect.left + rect.width / 2, y: rect.top, text, blockIds: [...new Set(ids)] };
  }
  function hideSelMenu() { selMenu = null; }
  // Clicking anywhere inside the sheet that is NOT a critic highlight closes
  // the IV popup; clicks on a <mark> bubble to its own openIVPopup handler.
  function onSheetClick(e) {
    if (!e.target.closest('.iv-mark')) hideIVPopup();
  }
  function selBlockId() { return selMenu?.blockIds?.length ? selMenu.blockIds[0] : null; }
  function selFromSheet(action) {
    if (!selMenu) return;
    const id = selBlockId();
    if (id == null) return;
    const b = chapterBlocks.find(x => x.id === id);
    if (!b) return;
    hideSelMenu();
    if (action === 'edit') startBlockEdit(b);
    else if (action === 'revise') startBlockRevise(b);
    else if (action === 'insertAfter') startBlockInsert(b.id);
    else if (action === 'delete') deleteBlock(b);
  }

  // —— Trope-issue dots (fact-check red / bestseller-critic violet) ————
  // ch.trope_issues is a per-(scene_pos, trope_index) roll-up rebuilt by the
  // backend whenever the pending conflict or the critic analysis changes. Each
  // story block knows its owning unit, so we paint small colored dots on the
  // affected blocks; clicking one opens an InfoTip popover (same style as the
  // SmartSelect/SmartCombobox summary tips) listing the exact issue wordings.
  function tropeIssueTipText(g, kind) {
    if (!g) return '';
    const items = (kind === 'fact' ? g.fact : g.critic) || [];
    const head = $t(kind === 'fact' ? 'writing.tropeDots.factTitle' : 'writing.tropeDots.criticTitle');
    return head + '\n\n' + items.map((s, i) => (i + 1) + '. ' + s).join('\n');
  }
  // Red dot shown inside the "Scene N starts here" chip when the FIRST trope
  // unit of that scene carries a fact-check finding — so the reader sees the
  // problem at the scene boundary too, not only next to the paragraph block.
  function firstUnitIssuesOfScene(pos) {
    if (pos == null || !ch?.trope_issues?.length) return null;
    let best = null;
    for (const g of ch.trope_issues) {
      if (g.scene_pos !== pos) continue;
      if (!best || (g.trope_index ?? 0) < (best.trope_index ?? 0)) best = g;
    }
    return best;
  }
  // Trope-issue dots beside a story block. Backend roll-ups (trope_issues) are
  // keyed by the intervention base: trope_index is an index INTO the scene's
  // TropeUnits (0-based), while blocks expose TropeIndex 1-based (ui+1).
  // Normalize the block meta to 0-based before matching; fall back to the raw
  // value when a group does not exist under the normalized key (defensive for
  // legacy data written with the old basis).
  const tropeIssueKey = (b) => (b.scene_pos >= 0 && b.trope_index != null ? b.scene_pos + ':' + (b.trope_index - 1 >= 0 ? b.trope_index - 1 : b.trope_index) : null);
  function tropeIssuesFor(b) {
    const k = tropeIssueKey(b);
    if (!k || !ch?.trope_issues?.length) return null;
    const g = ch.trope_issues.find(x => (x.scene_pos + ':' + x.trope_index) === k);
    if (g) return g;
    // Legacy fallback: same lookup without the 0-based normalization.
    const kRaw = b.scene_pos + ':' + b.trope_index;
    return ch.trope_issues.find(x => (x.scene_pos + ':' + x.trope_index) === kRaw) || null;
  }
  let insertText = '';

  function startBlockEdit(b) {
    selectedBlockId = b.id;
    editingBlockId = b.id;
    editingText = b.text;
    revisingBlockId = null;
    insertAfterId = null;
  }
  function startBlockRevise(b) {
    selectedBlockId = b.id;
    revisingBlockId = b.id;
    blockFeedback = '';
    editingBlockId = null;
    insertAfterId = null;
  }
  function startBlockInsert(afterId) {
    insertAfterId = afterId;
    insertBeforeId = null;
    insertText = '';
    editingBlockId = null;
    revisingBlockId = null;
  }
  // "Add subscene before": opens the same inline insert editor, but anchored
  // BEFORE the given block. The backend InsertBlockAfter treats after_id=0 as
  // "at the beginning", so any positive id - 1 maps to "before that block".
  function startBlockInsertBefore(blockId) {
    insertBeforeId = blockId;
    insertAfterId = null;
    insertText = '';
    editingBlockId = null;
    revisingBlockId = null;
  }
  function cancelBlockOps() {
    editingBlockId = null;
    revisingBlockId = null;
    insertAfterId = null;
    insertBeforeId = null;
  }

  async function saveBlockEdit(confirmed = false) {
    if (editingBlockId == null || !editingText.trim() || !ch) return;
    if (confirmed !== true && blockFacts(editingBlockId).length) {
      confirmAction($t('facts.confirm') + '\n' + blockFacts(editingBlockId).map(f => f.content).join('\n'), () => saveBlockEdit(true)); return;
    }
    try {
      const full = await api('PUT', `/api/chapters/${ch.num}/blocks/${editingBlockId}`, { text: editingText }, factHeaders(confirmed === true));
      applyChapter(ch.num, full);
      cancelBlockOps();
      addToast($t('writing.block.saved'), 'success');
    } catch (e) { addToast(e.message, 'error'); }
  }

  function deleteBlock(b) {
    if (!ch) return;
    confirmModal.set({
      message: $t('writing.block.deleteConfirm') + (blockFacts(b.id).length ? '\n' + $t('facts.confirm') + '\n' + blockFacts(b.id).map(f => f.content).join('\n') : ''),
      onConfirm: async () => {
        try {
          const full = await api('DELETE', `/api/chapters/${ch.num}/blocks/${b.id}`, null, factHeaders(true));
          applyChapter(ch.num, full);
          addToast($t('writing.block.deleted'), 'success');
        } catch (e) { addToast(e.message, 'error'); }
      },
    });
  }

  async function saveBlockInsert() {
    if ((insertAfterId == null && insertBeforeId == null) || !insertText.trim() || !ch) return;
    try {
      // before_id inserts the new block right BEFORE the anchored block
      // ("Add subscene before" chip); after_id keeps the legacy semantics.
      const payload = insertBeforeId != null ? { before_id: insertBeforeId, text: insertText } : { after_id: insertAfterId, text: insertText };
      const full = await api('POST', `/api/chapters/${ch.num}/blocks`, payload, factHeaders(false));
      applyChapter(ch.num, full);
      cancelBlockOps();
      addToast($t('writing.block.inserted'), 'success');
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function submitBlockRevise(confirmed = false) {
    if (revisingBlockId == null || !blockFeedback.trim() || !ch) return;
    if (confirmed !== true && blockFacts(revisingBlockId).length) {
      confirmAction($t('facts.confirmAI') + '\n' + blockFacts(revisingBlockId).map(f => f.content).join('\n'), () => submitBlockRevise(true)); return;
    }
    try {
      await api('POST', `/api/chapters/${ch.num}/blocks/${revisingBlockId}/revise`, { feedback: blockFeedback }, factHeaders(confirmed === true));
      addToast($t('writing.block.reviseStarted'), 'info');
      cancelBlockOps();
    } catch (e) { addToast(e.message, 'error'); }
  }

  // —— Key-fact inline highlighter ("marcatextos") ————————————————————
  // Clicking a "Key fact" chip used to tint the WHOLE paragraph sky-blue via
  // .story-block-highlight. Now we locate the exact SENTENCE the fact refers
  // to (its reference quote when available, otherwise a fuzzy scan of the
  // block text against the fact content) and wrap only that span in a yellow
  // <mark class="fact-mark">. The active fact is cleared whenever the sheet
  // content changes so stale highlights never linger over edited prose.
  let activeFactBlockId = null;
  const splitSentences = (s) => s.split(/(?<=[.!?\u2026])\s+/);
  function factMarkSegments(text, blockId) {
    if (!activeFact || activeFactBlockId !== blockId || !text) return null;
    const ref = (activeFact.references || []).find(r => !r.stale && r.chapter === ch?.num && r.block_id === blockId);
    const cands = [ref?.quote, ...(ref?.quotes || []), activeFact.content].filter(x => x && String(x).trim());
    for (const raw of cands) {
      const q = normText(String(raw)).toLowerCase();
      if (q.length < 4) continue;
      // Exact containment inside one sentence → highlight that sentence.
      for (const sent of splitSentences(text)) {
        const ns = normText(sent).toLowerCase();
        if (!ns) continue;
        if (ns.includes(q) || q.includes(ns)) {
          const i = text.indexOf(sent);
          if (i >= 0) return [{ t: text.slice(0, i) }, { t: sent, fact: true }, { t: text.slice(i + sent.length) }].filter(s => s.t);
        }
      }
      // Fuzzy fallback: first long word run of the fact that appears verbatim
      // in the text marks its containing sentence.
      const words = q.replace(/[^\p{L}\p{N}\s]/gu, '').split(/\s+/).filter(Boolean);
      for (let w = Math.min(words.length, 8); w >= 3; w--) {
        const pre = words.slice(0, w).join(' ');
        const idx = normText(text).toLowerCase().indexOf(pre);
        if (idx >= 0) {
          // Map the normalized offset back onto the raw text by counting
          // collapsed-whitespace tokens (same technique as ivSegmentsFor).
          let nh = 0, ri = 0;
          while (nh < idx && ri < text.length) {
            if (/\s/.test(text[ri])) { while (ri < text.length && /\s/.test(text[ri])) ri++; nh++; }
            else { ri++; nh++; }
          }
          for (const sent of splitSentences(text)) {
            const si = text.indexOf(sent);
            if (si <= ri && ri < si + sent.length) {
              return [{ t: text.slice(0, si) }, { t: sent, fact: true }, { t: text.slice(si + sent.length) }].filter(s => s.t);
            }
          }
        }
      }
    }
    return null;
  }
  function toggleFactHighlight(fact, blockId) {
    knowledgeExpanded = true;
    if (activeFact && activeFact.id === fact.id && activeFactBlockId === blockId) {
      activeFact = null; activeFactBlockId = null; highlightedBlock = null; return;
    }
    activeFact = fact;
    activeFactBlockId = blockId;
    highlightedBlock = null; // no whole-paragraph blue tint anymore
  }

  // —— Pending bestseller-critic interventions (inline highlights) —————
  // After the chapter critique runs, proposals are auto-drafted (backend) and
  // arrive as TropeIntervention entries with status "pending". Each one is
  // located inside the sheet by fuzzy-matching its current_text/quote against
  // the live paragraph text; matches render as a violet <mark>. Clicking a
  // mark opens a floating popup showing the passage to replace (or delete)
  // and the intervention feedback, with Apply / Dismiss buttons wired to
  // POST /api/chapters/{num}/analysis/intervention-apply|dismiss.
  let pendingIVs = [];
  // Bumped whenever the pending-intervention signature changes so the sheet
  // re-renders its <mark> segments even when only the ANALYSIS changed while
  // the block list reference stayed identical (Svelte would otherwise keep
  // stale markup produced before the critic finished).
  let ivRev = 0;
  let lastIvSig = '';
  $: {
    const list = [];
    for (const s of chapterAnalysis?.suggestions || []) {
      (s.interventions || []).forEach((iv, k) => {
        if (iv.status === 'pending' && iv.chapter_num === ch?.num) list.push({ ...iv, sid: s.id, key: s.id + ':' + k });
      });
    }
    pendingIVs = list;
    const sig = list.map(iv => [iv.scene_pos, iv.trope_index, normText(iv.current_text || ''), normText(iv.quote || '')].join('|')).join(';');
    if (sig !== lastIvSig) { lastIvSig = sig; ivRev = ivRev + 1; }
  }
  // Debug aid: log why proposals may not be highlighting (missing report,
  // wrong status/chapter, or empty anchors).
  $: if (chapterAnalysis) {
    const all = (chapterAnalysis.suggestions || []).flatMap(s => s.interventions || []);
    console.info('[critic-iv]', {
      suggestions: (chapterAnalysis.suggestions || []).length,
      interventions: all.length,
      pendingOnSheet: pendingIVs.length,
      chNum: ch?.num,
      sample: all.slice(0, 3).map(iv => ({ st: iv.status, cn: iv.chapter_num, sp: iv.scene_pos, ti: iv.trope_index, cur: (iv.current_text || '').slice(0, 40), q: (iv.quote || '') })),
    });
  }
  let ivPopup = null; // {x,y,w,h,iv} — anchored to the clicked <mark> rect
  let ivDetail = null; // {iv,color} — full-suggestion modal (scrollable, viewport-safe)
  let applyingIV = false;

  // Subscene tiling fetched from GET /api/chapters/{num}/iv-locator:
  // units[scenePos][unitIdx] = [paraStart, paraEnd, unitText] (paragraph spans
  // into the chapter's split on "\n\n"). Used to confine critic marks to the
  // targeted subscene/beat paragraphs instead of tinting whole scenes when the
  // anchor text cannot be located inside a single block.
  let ivLocator = null; // [][][]string
  let ivLocatorNum = -1;
  async function loadIVLocator(num) {
    try {
      const res = await api('GET', `/api/chapters/${num}/iv-locator`);
      if (num === loadedNum) { ivLocator = res?.units || []; ivLocatorNum = num; }
    } catch (e) { /* marks degrade gracefully to the block-level tiers */ }
  }

  // Paragraph window of one scene/subscene in chapterContent coordinates.
  function ivUnitRange(scenePos, unitIdx) {
    const units = (ivLocator && ivLocatorNum === ch?.num) ? ivLocator[scenePos] : null;
    if (!units) return null;
    let u = null;
    if (unitIdx != null && unitIdx >= 0 && unitIdx < units.length) u = units[unitIdx];
    else if (units.length === 1) u = units[0];
    if (!u) return null;
    const sep = '\n\n';
    const paras = (chapterContent || '').split(sep);
    const a = Math.max(0, Math.min(Number(u[0]) || 0, paras.length));
    const b = Math.max(a, Math.min(Number(u[1]) || 0, paras.length));
    if (b <= a) return null;
    let start = 0;
    for (let i = 0; i < a; i++) start += paras[i].length + sep.length;
    let end = start;
    for (let i = a; i < b; i++) end += paras[i].length + (i < b - 1 ? sep.length : 0);
    return { start, end, text: paras.slice(a, b).join(sep), exact: unitIdx != null };
  }

  // Locate an IV's anchor inside its targeted subscene window only. Returns
  // normalized [lo,hi) offsets relative to the UNIT text (ivUnitRange.text),
  // or null. This is the precision tier that keeps highlights on the beat
  // where the change belongs, never the whole scene.
  function locateInIvUnit(iv) {
    if (!ivLocator || iv.scene_pos == null || iv.scene_pos < 0) return null;
    const r = ivUnitRange(iv.scene_pos, iv.trope_index);
    if (!r) return null;
    const hay = normText(r.text);
    if (!hay) return null;
    for (const c of [normText(iv.current_text), normText(iv.quote)].filter(x => x)) {
      const sp = locateSpan(hay, c);
      if (sp) return sp;
    }
    return null;
  }

  // Raw-text span [start,end) of one IV's targeted subscene/beat inside a
  // block's text, derived from the iv-locator paragraph window. Blocks are
  // paragraphs of the same chapter split: we locate this block among them and
  // clip it to the unit's [paraStart, paraEnd) range. When the beat covers the
  // whole block, the entire block text is returned (that IS the precise
  // subscene highlight); when the anchor sits mid-block we still return the
  // full block only if the unit fully contains it — otherwise null, letting
  // the caller fall back to in-block sentence-level matching. Null when the
  // block doesn't overlap the unit or the locator is unavailable/stale.
  function ivUnitSpanInBlock(text, iv) {
    if (!ivLocator || iv.scene_pos == null || iv.scene_pos < 0) return null;
    const r = ivUnitRange(iv.scene_pos, iv.trope_index);
    if (!r) return null;
    const sep = '\n\n';
    const paras = (chapterContent || '').split(sep);
    let pi = -1;
    for (let i = 0; i < paras.length; i++) {
      if (paras[i] === text) { pi = i; break; }
    }
    if (pi < 0) {
      // Sheet edits may differ in trailing whitespace only.
      for (let i = 0; i < paras.length; i++) {
        if (paras[i].trim() === String(text || '').trim()) { pi = i; break; }
      }
    }
    if (pi < 0 || pi >= r.end || pi < r.start) return null;
    // The block overlaps the unit window. A sheet paragraph never spans two
    // chapter paragraphs, so overlap means containment → mark exactly this
    // block (never sibling blocks of other beats).
    void sep;
    return [0, text.length];
  }

  // Normalize for fuzzy matching: collapse whitespace, strip soft hyphens.
  const normText = (s) => (s || '').replace(/\u00ad/g, '').replace(/\s+/g, ' ').trim();

  // Find [start,end) offsets of `needle` inside `hay` with graceful degradation:
  // exact → case-insensitive → first 8 words prefix match. Returns null if none.
  function locateSpan(hay, needle) {
    if (!hay || !needle) return null;
    let i = hay.indexOf(needle);
    if (i >= 0) return [i, i + needle.length];
    const lh = hay.toLowerCase(), ln = needle.toLowerCase();
    i = lh.indexOf(ln);
    if (i >= 0) return [i, i + needle.length];
    const words = needle.split(' ');
    for (let w = Math.min(words.length, 8); w >= 3; w--) {
      const pre = words.slice(0, w).join(' ');
      i = lh.indexOf(pre.toLowerCase());
      if (i >= 0) return [i, Math.min(hay.length, i + pre.length)];
    }
    // Last resort: the anchor's FIRST sentence alone — current_text spans a
    // whole trope unit and is often truncated/reworded by the model.
    const firstSent = ln.split(/(?<=[.!?\u2026])\s/)[0] || '';
    if (firstSent.length >= 25) {
      i = lh.indexOf(firstSent);
      if (i >= 0) return [i, Math.min(hay.length, i + firstSent.length)];
    }
    return null;
  }

  // Split one sheet paragraph into plain/highlight segments for all pending IVs
  // that target it. Later matches on already-highlighted ranges are skipped.
  // NOTE on indexing bases: interventions carry trope_index as an index INTO
  // Scene.TropeUnits (0-based, see buildTropeUnits / draftedTargetValidator),
  // while story blocks expose TropeIndex 1-based (ui+1 in SyncChapterBlocks).
  // Always normalize the block meta to 0-based before comparing with an IV.
  function ivSegmentsFor(text, blockId) {
    const n = normText(text);
    if (!n || !pendingIVs.length) return null;
    const meta = blockMetaById().get(blockId);
    if (!meta || meta.scene_pos == null || meta.scene_pos < 0) return null;
    const metaTrope0 = meta.trope_index != null ? meta.trope_index - 1 : null;
    const marks = [];
    // A beat can legitimately carry MORE THAN ONE critic suggestion (the
    // backend dedups by exact advice-on-a-beat, not by beat). When two IVs
    // resolve to overlapping spans we FUSE the ranges and accumulate every
    // owning IV on the same mark, so both suggestions stay reachable from a
    // single click (the popup then lists them all). The old behavior — skip
    // any overlapping mark — silently dropped the second suggestion.
    const addMark = (sp, iv) => {
      if (!sp) return;
      const lo = Math.max(0, Math.min(sp[0], normText(text).length));
      const hi = Math.max(lo, Math.min(sp[1], normText(text).length));
      if (hi <= lo) return;
      const ov = marks.filter(m => lo < m.hi && m.lo < hi);
      if (ov.length) {
        const m0 = ov[0];
        m0.lo = Math.min(m0.lo, lo);
        m0.hi = Math.max(m0.hi, hi);
        for (const extra of ov.slice(1)) {
          m0.lo = Math.min(m0.lo, extra.lo);
          m0.hi = Math.max(m0.hi, extra.hi);
          const at = marks.indexOf(extra);
          if (at >= 0) marks.splice(at, 1);
        }
        if (!m0.ivs.some(x => x.key === iv.key)) m0.ivs.push(iv);
        return;
      }
      marks.push({ lo, hi, iv, ivs: [iv] });
    };
    const sceneBlocks = chapterBlocks.filter(b => b.scene_pos === meta.scene_pos);
    const firstOfScene = sceneBlocks.length ? sceneBlocks[0].id : null;
    // First block of the subscene/beat unit this IV targets. Must be computed
    // from the IV's OWN unit index: using the current block's index made Tier 4
    // attach the mark to whichever block was being rendered, so EVERY paragraph
    // of the unit claimed to be the target and no highlight survived the
    // overlap-skip (i.e. nothing highlighted at all).
    const firstOfUnitFor = (ivUnit0) => {
      for (const sb of sceneBlocks) {
        const t0 = sb.trope_index != null ? sb.trope_index - 1 : null;
        if (ivUnit0 == null) return sb.id; // no unit info: first block of scene
        if (t0 == null || !sb.trope_count || sb.trope_count <= 1 || t0 === ivUnit0) return sb.id;
      }
      return null;
    };
    const sceneHay = normText(sheetSceneProse(meta.scene_pos));
    const chapHay = normText(chapterContent || '');
    // Locator-precision state: with a fresh iv-locator payload for the current
    // chapter, marks are confined to the targeted subscene paragraphs.
    const locatorFresh = !!(ivLocator && ivLocatorNum === ch?.num);
    for (const iv of pendingIVs) {
      if (iv.scene_pos !== meta.scene_pos) continue;
      const sameUnit = iv.trope_index != null && metaTrope0 != null && meta.trope_count > 1 && metaTrope0 !== iv.trope_index;
      if (sameUnit) continue;
      const cand = [normText(iv.current_text), normText(iv.quote)].filter(x => x);
      let hit = false;
      // Tier 1: anchor lands inside THIS block's text (exact/ci/prefix match).
      for (const c of cand) {
        const sp = locateSpan(n, c);
        if (sp) { addMark(sp, iv); hit = true; break; }
      }
      if (hit) continue;
      // Tier 2 (subscene-precise): confine the mark to the beat paragraphs the
      // critic actually targets. Two locator paths:
      //  a) anchor found INSIDE the unit window → re-locate that fragment in
      //     this block so only the matching sentence(s) get tinted;
      //  b) anchor not locatable but this block lies within the unit's
      //     paragraph window → mark exactly this block (never sibling beats).
      // When the locator is available it REPLACES the old whole-scene tier:
      // blocks outside the targeted subscene are never highlighted — that was
      // the bug tinting entire scenes instead of the targeted beat.
      if (locatorFresh) {
        const usp = ivUnitSpanInBlock(text, iv);
        if (usp) {
          const r = ivUnitRange(iv.scene_pos, iv.trope_index);
          const hay = normText(r.text);
          let placed = false;
          for (const c of cand) {
            const sp = locateSpan(hay, c);
            if (!sp) continue;
            const frag = hay.slice(sp[0], sp[1]);
            const bs = locateSpan(n, frag);
            if (bs) { addMark(bs, iv); placed = true; break; }
          }
          if (placed) { hit = true; continue; }
          addMark([Math.min(usp[0], n.length), Math.min(usp[1], n.length)], iv);
          hit = true; continue;
        }
        // The locator knows this IV's beat window and this block is NOT part
        // of it → skip every loose scene-wide/chapter-wide tier.
        if (ivUnitRange(iv.scene_pos, iv.trope_index)) continue;
      }
      // Tier 2-legacy (no locator for this chapter yet): anchor lives somewhere
      // in this block's SCENE prose (positional proof via iv.scene_pos).
      // Re-locate loosely against the block; if only the unit-level target
      // matches, highlight the whole single-unit block.
      for (const c of cand) {
        if (sceneHay && locateSpan(sceneHay, c)) {
          const pre = c.split(' ').slice(0, 6).join(' ');
          const sp = locateSpan(n, pre) || locateSpan(n, (c.split(/(?<=[.!?\u2026])\s/)[0] || ''));
          if (sp) { addMark(sp, iv); hit = true; break; }
          if (iv.trope_index == null || !meta.trope_count || meta.trope_count <= 1) { addMark([0, n.length], iv); hit = true; break; }
        }
      }
      if (hit) continue;
      // Tier 3: anchor exists somewhere in the chapter (stale scene_pos?) —
      // mark this block's opening sentence so the suggestion stays reachable.
      for (const c of cand) {
        if (c && chapHay.includes(c)) {
          const sent = n.split(/(?<=[.!?\u2026])\s/)[0] || '';
          if (sent.length >= 20) addMark([0, Math.min(n.length, sent.length)], iv);
          else addMark([0, n.length], iv);
          hit = true;
          break;
        }
      }
      if (hit) continue;
      // Tier 4 (positional fallback): the anchor text can't be located
      // anywhere (reworded prose, truncated quote). Still show it IN CONTEXT,
      // Grammarly-style: attach the mark to the first block of the targeted
      // trope unit (or of the scene when the IV has no unit index), so the
      // critic's feedback never disappears into the report-only section at
      // the bottom of the page. With a fresh locator this tier only fires for
      // IVs whose beat window is unavailable (out-of-range indexes).
      const ivUnit0 = iv.trope_index != null ? iv.trope_index : null;
      const isTargetBlock = blockId === firstOfUnitFor(ivUnit0)
        || (ivUnit0 == null && blockId === firstOfScene);
      if (isTargetBlock) addMark([0, n.length], iv);
    }
    if (!marks.length) return null;
    marks.sort((a, b) => a.lo - b.lo);
    // Map normalized offsets back onto the raw text via proportional scan of
    // collapsed-whitespace boundaries (good enough for display splitting).
    const segs = [];
    let last = 0, nh = 0;
    const raw = text;
    const advance = (to) => { // move nh forward until reaching offset `to`, return raw index
      let ri = last;
      while (nh < to && ri < raw.length) {
        const ch = raw[ri];
        if (/\s/.test(ch)) {
          let rj = ri; while (rj < raw.length && /\s/.test(raw[rj])) rj++;
          nh += 1; ri = rj;
        } else { nh += 1; ri += 1; }
      }
      return ri;
    };
    for (const m of marks) {
      const rs = advance(m.lo);
      const re = advance(m.hi);
      if (rs > last) segs.push({ t: raw.slice(last, rs) });
      segs.push({ t: raw.slice(rs, re), iv: true, ivKey: m.iv ? m.iv.key : null, ivKeys: (m.ivs || [m.iv]).map(x => x.key) });
      last = re;
    }
    if (last < raw.length) segs.push({ t: raw.slice(last) });
    return segs;
  }

  // Concatenated prose of one scene straight from the blocks (no server call
  // needed): used as the tier-2 haystack for intervention matching.
  function sheetSceneProse(pos) {
    return chapterBlocks.filter(b => b.scene_pos === pos).map(b => b.text).join('\n\n');
  }

  // Distinct highlighter color per suggestion: a deterministic hash of the
  // intervention's stable key maps it onto a fixed palette, so every pending
  // critic suggestion gets its own inline <mark> tint (Grammarly-style) that
  // stays identical across re-renders and resyncs.
  const IV_COLORS = ['#f9d342', '#7dd3fc', '#86efac', '#f9a8d4', '#c4b5fd', '#fca5a5'];
  function ivColorIdx(key) {
    let h = 0;
    for (let i = 0; i < (key || '').length; i++) h = (h * 31 + key.charCodeAt(i)) >>> 0;
    return h % IV_COLORS.length;
  }
  function ivMarkStyle(key) {
    const c = IV_COLORS[ivColorIdx(key)];
    // Solid highlighter tint + matching underline, both forced with !important:
    // the user theme's `mark`/secondary styles (the old lavender look) must
    // never override the per-suggestion color. The popup and the full-detail
    // modal read the exact same ivColor() hash → card theme always equals the
    // inline highlight it belongs to.
    return `background:${c}59 !important;color:inherit !important;border-bottom:1.5px solid ${c} !important;`;
  }
  // Fused mark style for a beat carrying MORE THAN ONE suggestion: every
  // owning intervention contributes its own underline (stacked) so both
  // colors stay visible on the shared highlight, plus a superscript count
  // badge rendered next to the mark in the sheet template.
  function ivMarkStyleMulti(keys) {
    if (!keys || keys.length <= 1) return ivMarkStyle(keys && keys[0]);
    const cs = keys.map(k => IV_COLORS[ivColorIdx(k)]);
    const bg = cs[0];
    const borders = cs.map(c => `2px solid ${c}`).join(', ');
    return `background:${bg}4d !important;color:inherit !important;`
      + `border-bottom:${borders} !important;padding-bottom:3px !important;`;
  }
  // Solid color of a suggestion — shared by the inline <mark>, the popup card
  // theme and the full-detail modal so all three always match.
  function ivColor(ivOrKey) {
    const key = typeof ivOrKey === 'string' ? ivOrKey : (ivOrKey && (ivOrKey.key || ivKdFallback(ivOrKey))) || '';
    return IV_COLORS[ivColorIdx(key)] || IV_COLORS[0];
  }
  function ivKdFallback(iv) { return iv ? `${iv.scene_pos}:${iv.trope_index}` : ''; }

  // The <mark> segments now carry their owning intervention's key (ivKey), so
  // on click we recover the EXACT pending suggestion instead of fuzzy-matching
  // the visible fragment (which could surface the wrong one). findIvForSegment
  // remains only as a defensive fallback for legacy marks without a key.
  function findIvForKey(ivKey) {
    if (!ivKey) return null;
    return pendingIVs.find(x => x.key === ivKey) || null;
  }
  // Recover EVERY intervention owning a fused mark (a beat with two or more
  // suggestions). Order follows the mark's key list; unknown keys are skipped.
  function findIvsForKeys(ivKeys, segText) {
    const list = [];
    for (const k of ivKeys || []) {
      const iv = findIvForKey(k);
      if (iv && !list.some(x => x.key === iv.key)) list.push(iv);
    }
    if (!list.length) {
      const iv = findIvForSegment(segText);
      if (iv) list.push(iv);
    }
    return list;
  }

  // Popup placement: measure the card after render (popupBox action) and clamp
  // it inside the viewport, flipping below the mark when there is no room
  // above. This replaces the old fixed translate(-50%,-100%) that let tall
  // suggestions overflow off-screen with content unreachable.
  let popupBox = null; // {w,h} measured from the DOM node
  function popupMeasure(node) {
    const m = () => { popupBox = { w: node.offsetWidth, h: node.offsetHeight }; };
    m();
    if (typeof ResizeObserver !== 'undefined') {
      const ro = new ResizeObserver(m); ro.observe(node);
      return { destroy() { ro.disconnect(); } };
    }
    return {};
  }
  function popupPos(p) {
    const vw = typeof window !== 'undefined' ? window.innerWidth : 1280;
    const vh = typeof window !== 'undefined' ? window.innerHeight : 800;
    const W = Math.min(360, vw * 0.9);
    // Taller card when several suggestions share the beat (tabs + list).
    const H = popupBox ? popupBox.h : (p.ivs && p.ivs.length > 1 ? 320 : 240);
    const gap = 10;
    let top = p.y - H - gap;                 // prefer above the mark
    if (top < 8) top = Math.min(p.y + p.h + gap, vh - H - 8); // flip below
    if (top < 8) top = 8;
    let left = p.x - W / 2;                  // center over the mark
    left = Math.max(8, Math.min(left, vw - W - 8));
    return `left:${left}px;top:${top}px;width:${W}px;max-height:${Math.max(140, vh - 16)}px;`
      + `overflow-y:auto;`;
  }

  function openIVPopup(e, ivs) {
    e.preventDefault(); e.stopPropagation();
    const arr = Array.isArray(ivs) ? ivs : [ivs];
    if (!arr.length) return;
    const rect = e.currentTarget.getBoundingClientRect();
    ivPopup = { x: rect.left + rect.width / 2, y: rect.top, w: rect.width, h: rect.height,
                iv: arr[0], ivs: arr, active: 0 };
  }
  function popupActiveIv() {
    if (!ivPopup) return null;
    const list = ivPopup.ivs || [ivPopup.iv];
    return list[ivPopup.active] || list[0];
  }
  function hideIVPopup() { ivPopup = null; ivDetail = null; }
  function openIVDetail() {
    if (!ivPopup) return;
    const iv = popupActiveIv();
    if (!iv) return;
    ivDetail = { iv: iv, color: ivColor(iv.key || iv), all: ivPopup.ivs || [iv] };
  }

  async function applyPendingIV(confirmed = false) {
    if (!ch || applyingIV) return;
    // Works from the popup OR the full-detail modal (the modal closes the
    // popup strip but keeps ivDetail — Apply/Dismiss must still resolve).
    // With a fused mark (multiple suggestions on one beat) we act on the
    // ACTIVE tab's intervention only, never on all of them at once.
    const iv = (ivPopup && popupActiveIv()) || (ivDetail && ivDetail.iv);
    if (!iv) return;
    applyingIV = true;
    try {
      const res = await api('POST', `/api/chapters/${ch.num}/analysis/intervention-apply`, { id: iv.sid, iv: pendingIvIndex(iv) }, factHeaders(confirmed === true));
      if (res.chapter) applyChapter(ch.num, res.chapter);
      hideIVPopup();
      addToast($t('writing.iv.applied'), 'success');
    } catch (e) {
      if (String(e.message || '').includes('fact_impact_confirm') && confirmed !== true) {
        confirmAction($t('facts.confirmAI'), () => applyPendingIV(true));
      } else {
        addToast(e.message, 'error');
      }
    } finally { applyingIV = false; }
  }
  async function dismissPendingIV() {
    if (!ch) return;
    const iv = (ivPopup && popupActiveIv()) || (ivDetail && ivDetail.iv);
    if (!iv) return;
    try {
      await api('POST', `/api/chapters/${ch.num}/analysis/intervention-dismiss`, { id: iv.sid, iv: pendingIvIndex(iv) });
      // Keep the popup open when sibling suggestions remain on the same beat:
      // drop only the dismissed one and re-point the active index. The sheet
      // re-renders via ivRev so the mark loses this color/entry automatically.
      if (ivPopup && ivPopup.ivs && ivPopup.ivs.length > 1) {
        const rest = ivPopup.ivs.filter(x => x.key !== iv.key);
        if (rest.length) {
          ivPopup = { ...ivPopup, ivs: rest, iv: rest[0], active: 0 };
          addToast($t('writing.iv.dismissed'), 'info');
          return;
        }
      }
      hideIVPopup();
      addToast($t('writing.iv.dismissed'), 'info');
    } catch (e) { addToast(e.message, 'error'); }
  }
  function pendingIvIndex(iv) {
    const s = (chapterAnalysis?.suggestions || []).find(x => x.id === iv.sid);
    if (!s) return 0;
    const list = s.interventions || [];
    // Exact identity first: with multiple suggestions anchored on the SAME
    // beat, a positional (scene_pos/trope_index) lookup could resolve to the
    // wrong entry. The key is `s.id + ':' + k` — the index into the very same
    // list we are addressing here — so parse it as the primary locator.
    let i = list.findIndex(x => x.key === iv.key);
    if (i < 0 && typeof iv.key === 'string') {
      const pfx = iv.sid + ':';
      if (iv.key.startsWith(pfx)) {
        const n = Number(iv.key.slice(pfx.length));
        if (Number.isInteger(n) && n >= 0 && n < list.length) return n;
      }
    }
    if (i < 0) i = list.findIndex(x => x.scene_pos === iv.scene_pos && x.trope_index === iv.trope_index);
    return Math.max(i, 0);
  }

  // The <mark> segments only carry their visible text, so on click we recover
  // the intervention whose current_text/quote fuzzy-matches the highlighted
  // fragment inside this block.
  function findIvForSegment(segText) {
    const n = normText(segText);
    if (!n) return null;
    for (const iv of pendingIVs) {
      const cand = [normText(iv.current_text), normText(iv.quote)].filter(Boolean);
      if (cand.some(c => c === n || c.startsWith(n) || n.startsWith(c))) return iv;
    }
    return pendingIVs[0] || null;
  }

  // Popup header describing what the intervention will do to the passage.
  function ivActionLabelHtml(iv) {
    if (!iv) return '';
    const action = String(iv.action || '').toLowerCase();
    let key = 'writing.iv.actionRewrite';
    if (action.includes('delete') || action.includes('remove') || action.includes('cut')) key = 'writing.iv.actionDelete';
    else if (action.includes('insert') || action.includes('add')) key = 'writing.iv.actionInsert';
    const label = $t(key);
    const repl = (iv.replacement || iv.proposed_text || '').replace(/&/g, '&amp;').replace(/</g, '&lt;');
    return '<b>' + label + '</b>' + (repl ? '<div class="mt-1 italic">“' + repl + '”</div>' : '');
  }

  // During streaming $streamingContent only holds the tail window (perf guard); full text is fetched after generation ends
  $: displayContent = isStreamingThis ? $streamingContent : chapterContent;
  $: chapterWordCount = ch?.word_count || (chapterContent ? countProseUnits(chapterContent) : 0);
  $: showTaskTokens = $taskRunning && isCurrent;
  $: totalWords = chapters.reduce((sum, c) => sum + (c.word_count || 0), 0);

  $: foreshadows = p?.foreshadows || [];
  $: fsActive = foreshadows.filter(f => f.status === 'planted' || f.status === 'progressing');
  $: fsOverdue = fsActive.filter(f => f.target_chapter > 0 && (currentIdx + 1) > f.target_chapter);
  $: fsNearTarget = fsActive.filter(f =>
    f.target_chapter > 0 && (currentIdx + 1) >= f.target_chapter - 2 && (currentIdx + 1) <= f.target_chapter
  );
  $: writingConflict = p?.pending_writing_conflict || null;
  // Scene-plan review gate: the backend pauses chapter generation after
  // planning scenes (unless automation is on) and persists the pending
  // chapter number. The beat-sheet editor now lives INLINE in the
  // "Scene matrix" tab, so a pending plan just selects that chapter and
  // keeps the user on tab 1 (no modal to open anymore).
  $: scenePlanPendingNum = p?.pending_scene_plan_chapter || 0;
  let scenePlanChapter = null;
  $: if (scenePlanPendingNum && !$taskRunning) {
    if (scenePlanChapter !== scenePlanPendingNum) {
      scenePlanChapter = scenePlanPendingNum;
      addToast($t('writing.scenePlan.pendingToast', { num: scenePlanPendingNum }), 'info');
    }
    // Select the pending chapter exactly once per gate, so the auto-selection
    // never fights with the user while they work on other chapters.
    const pi = chapters.findIndex(c => c.num === scenePlanPendingNum);
    if (pi >= 0 && lastAutoSelectedPlanChapter !== scenePlanPendingNum) {
      lastAutoSelectedPlanChapter = scenePlanPendingNum;
      selectChapter(pi);
    }
    writingTab = 1;
  }
  let lastAutoSelectedPlanChapter = null;
  $: scenePlanEditorNum = ch?.num ?? scenePlanChapter ?? scenePlanPendingNum ?? null;

  // —— Scene-plan cache for the SCENE chip tooltips (tab 2) ——————————————————
  // Only SCENE chips carry a tooltip (the black SmartSelect-style popover with
  // that scene's description); subscene chips never do. The block-synced meta
  // already carries title/description when available, but chapters drafted
  // before that field existed (or whose plan lives only server-side) fall back
  // to this lazily-loaded cache keyed by chapter number.
  let planByChapter = {};
  async function ensurePlanFor(num) {
    if (!num || planByChapter[num] !== undefined) return;
    planByChapter[num] = null; // mark "in flight / done" so we fetch once
    try {
      const res = await api('GET', `/api/chapters/${num}/scene-plan`);
      planByChapter = { ...planByChapter, [num]: res?.plan?.scenes || [] };
    } catch { /* no plan — chips simply show no tooltip */ }
  }
  // Warm the cache for the selected chapter while on the writing tab.
  $: if (writingTab === 2 && ch?.num) ensurePlanFor(ch.num);
  // Prefetch plans for every non-accepted chapter that has a beat sheet, so
  // scene tooltips work even when browsing chapters without selecting them.
  $: if (writingTab === 2 && chapters.length) {
    for (const c of chapters) if (c.scene_plan?.scenes?.length) ensurePlanFor(c.num);
  }
  $: orphanWriting = !!(ch && isCurrent && ch.status === 'writing' && !writingConflict && !$taskRunning);

  // --- Bestseller-editor critique passes (siblings of the outline one) ---
  // Scene-plan pass: one report over all PENDING beat sheets.
  $: scenePlanAnalysis = p?.scene_plan_analysis || null;
  $: scenePlanPendingCount = chapters.filter(c => c.status !== 'accepted' && c.scene_plan?.scenes?.length).length;
  // Act pass: reports keyed by pre-planning act number, rendered newest first.
  $: actAnalyses = Object.entries(p?.act_analyses || {})
    .map(([no, rep]) => ({ no: Number(no), rep }))
    .filter(x => x.rep)
    .sort((a, b) => b.no - a.no);
  // Chapter pass: report lives on the selected chapter itself.
  $: chapterAnalysis = ch?.analysis || null;
  // Pre-planning acts (loaded once in onMount) drive the manual act-critic
  // buttons; only acts whose last chapter is already accepted can be judged,
  // since the critic reviews drafted prose.
  let preplanActs = [];
  function critiqueAct(no) {
    api('POST', '/api/acts/analyze', { act_no: no })
      .then(() => addToast($t('act.analysis.toasts.started'), 'info'))
      .catch((e) => addToast(e.message, 'error'));
  }
  // Pre-plan acts that END right after chapter `num` — used to interleave the
  // manual act-critique "steps" inside ChapterSidebarList at every point where
  // one act hands over to the next (and after the last chapter for the final
  // act, when its end_ch reaches or exceeds the chapter list).
  function actsEndingAfter(num) {
    if (!num) return [];
    return preplanActs.filter(a => a.end_ch === num || (a.no === preplanActs.length && num >= a.end_ch && a.end_ch > 0));
  }

  async function resolveWritingConflict(action) {
    if ($taskRunning) return;
    try {
      const res = await api('POST', '/api/chapter/conflict-resolve', { action });
      if (action === 'retry') {
        progress.set(await api('GET', '/api/progress'));
        await api('POST', '/api/chapter/generate');
        addToast($t('writing.toasts.generateStarted', { num: writingConflict?.chapter_num || ch?.num }), 'info');
        return;
      }
      progress.set(res);
      // dismiss ≡ force_review on the server
      if (action === 'force_review' || action === 'dismiss') {
        addToast($t('writing.conflict.forceReview'), 'success');
      } else if (action === 'revise_from_feedback') {
        // Server started a detached task (202 + saved progress snapshot).
        addToast($t('writing.conflict.reviseFromFeedbackStarted', { num: writingConflict?.chapter_num || ch?.num }), 'info');
      } else if (action === 'surgical_trope') {
        // Server started a detached surgical repair + re-verification task.
        progress.set(await api('GET', '/api/progress'));
        addToast($t('writing.conflict.surgicalTropeStarted', { num: writingConflict?.chapter_num || ch?.num }), 'info');
      }
    } catch (e) {
      addToast(e.message, 'error');
    }
  }

  function gotoOutlineForConflict() {
    const num = writingConflict?.chapter_num || ch?.num;
    if (num) {
      try { sessionStorage.setItem(OUTLINE_FOCUS_KEY, String(num)); } catch {}
    }
    navigate('outline');
  }

  function gotoForeshadows() {
    navigate('foreshadows');
  }

  $: statusMeta = {
    pending:  { label: $t('writing.status.pending'),  cls: 'badge-ghost',   dot: 'bg-base-content/20' },
    writing:  { label: $t('writing.status.writing'),  cls: 'badge-warning', dot: 'bg-warning animate-pulse' },
    review:   { label: $t('writing.status.review'),   cls: 'badge-info',    dot: 'bg-info' },
    accepted: { label: $t('writing.status.accepted'), cls: 'badge-success', dot: 'bg-success' },
  };

  let reviseFeedback = '';
  let showRevise = false;
  let contentEl;
  let reviseTextareaEl;
  let hasPolishSkills = false;

  // Floating "quote" button after selecting source text; null means hidden
  let quotePopover = null;

  function checkContentSelection() {
    const sel = window.getSelection();
    if (!sel || sel.isCollapsed || sel.rangeCount === 0) { quotePopover = null; return; }
    const text = sel.toString().trim();
    if (!text || text.length < 2) { quotePopover = null; return; }
    const range = sel.getRangeAt(0);
    if (!contentEl || !contentEl.contains(range.commonAncestorContainer)) {
      quotePopover = null; return;
    }
    // The continuous sheet has its OWN floating menu (edit/revise/insert/
    // delete). Suppress the legacy single-button quote popover there so both
    // never fight over the same selection. Use the bound reference (sheetEl)
    // instead of getElementById so it keeps working even if the id changes.
    if (sheetEl && sheetEl.contains(range.commonAncestorContainer)) {
      quotePopover = null; return;
    }
    const rect = range.getBoundingClientRect();
    if (rect.width === 0 && rect.height === 0) { quotePopover = null; return; }
    quotePopover = {
      text,
      x: rect.left + rect.width / 2,
      y: rect.top,
    };
  }

  function hideQuotePopover() { quotePopover = null; }

  function insertQuoteToFeedback() {
    if (!quotePopover) return;
    const text = quotePopover.text;
    const quoteLine = `> ${text}`;
    // If the user already wrote feedback, ensure a blank line separates the quote from it
    const current = reviseFeedback;
    let insertion;
    if (current === '') {
      insertion = quoteLine + '\n';
    } else if (current.endsWith('\n')) {
      insertion = quoteLine + '\n';
    } else {
      insertion = '\n' + quoteLine + '\n';
    }
    // Prefer inserting at the textarea cursor; otherwise append at the end
    const ta = reviseTextareaEl;
    if (ta && document.activeElement === ta && ta.selectionStart != null) {
      const start = ta.selectionStart;
      const end = ta.selectionEnd;
      reviseFeedback = current.slice(0, start) + insertion + current.slice(end);
      requestAnimationFrame(() => {
        ta.focus();
        const pos = start + insertion.length;
        ta.setSelectionRange(pos, pos);
      });
    } else {
      reviseFeedback = current + insertion;
      requestAnimationFrame(() => {
        if (ta) { ta.focus(); const pos = reviseFeedback.length; ta.setSelectionRange(pos, pos); }
      });
    }
    showRevise = true;
    addToast($t('writing.toasts.quoteInserted', { n: text.length }), 'success');
    // Clear the selection and hide the button
    const sel = window.getSelection();
    if (sel) sel.removeAllRanges();
    hideQuotePopover();
  }

  // Auto-scroll to bottom while streaming: coalesced into rAF, at most once per frame, to avoid forced reflows
  let scrollPending = false;
  function scheduleScroll() {
    if (scrollPending) return;
    scrollPending = true;
    requestAnimationFrame(() => {
      scrollPending = false;
      if (contentEl) contentEl.scrollTop = contentEl.scrollHeight;
    });
  }
  $: if (isStreamingThis && contentEl) scheduleScroll();

  function selectChapter(i) {
    if (savingKnowledge) return;
    selectedSettings = [];
    showKnowledge = false;
    selectedChapter.set(i);
    maybeLoadContent(chapters[i]);
    showRevise = false;
    reviseFeedback = '';
    hideQuotePopover();
    cancelBlockOps();
  }

  async function doGenerate() {
    try {
      await api('POST', '/api/chapter/generate');
      addToast($t('writing.toasts.generateStarted', { num: ch?.num }), 'info');
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function doConfirm() {
    try {
      await api('POST', '/api/chapter/confirm');
      progress.set(await api('GET', '/api/progress'));
      addToast($t('writing.toasts.confirmed', { num: ch?.num }), 'success');
      // Jump to next chapter
      const next = await api('GET', '/api/progress');
      if (next.current_chapter_index < (next.chapters || []).length) {
        selectedChapter.set(next.current_chapter_index);
      }
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function doRevise(confirmed = false) {
    const fb = reviseFeedback.trim();
    if (!fb && !selectedSettings.length) { addToast($t('writing.toasts.feedbackRequired'), 'error'); return; }
    if (!ch) return;
    const num = ch.num;
    const rev = loadedRev;
    if (selectedSettings.length && confirmed !== true) {
      try {
        const knowledge = await api('GET', '/api/knowledge?chapter=' + num);
        if (ch?.num !== num || loadedRev !== rev) return;
        if (knowledge.facts?.length) {
          confirmAction($t('writing.knowledge.confirm') + '\n' + knowledge.facts.map(f => f.content).join('\n'), () => {
            if (ch?.num === num && loadedRev === rev) doRevise(true);
          });
          return;
        }
      } catch (e) { addToast(e.message, 'error'); return; }
    }
    const body = { feedback: fb, worldview_ids: selectedSettings };
    try {
      if (!selectedSettings.length && isCurrent && ch.status === 'review') {
        // Current under-review chapter: full revision flow
        await api('POST', '/api/chapter/revise', body, factHeaders(confirmed === true));
      } else {
        // Other chapters (incl. confirmed): targeted minimal revision without touching other chapters
        await api('POST', '/api/chapter/revise/' + ch.num, body, factHeaders(confirmed === true));
      }
      addToast($t('writing.toasts.reviseStarted', { num: ch.num }), 'info');
      reviseFeedback = '';
      showRevise = false;
      showKnowledge = false;
      selectedSettings = [];
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function doPolish() {
    if (!ch) return;
    try {
      await api('POST', '/api/chapter/polish', { num: ch.num });
      addToast($t('writing.toasts.polishStarted', { num: ch.num }), 'info');
    } catch (e) { addToast(e.message, 'error'); }
  }

  // —— Draft recovery actions (resume / fact-check / reset) ————————
  // When a chapter generation was interrupted AFTER writing its prose (the
  // summary or critic step failed, the task deadline hit, or the user stopped
  // it), the backend keeps the prose as a resumable draft (draft_saved_at).
  // These buttons let the author finish, re-verify or discard that draft
  // without regenerating the expensive prose.
  async function doResumeDraft() {
    if (!ch) return;
    try {
      await api('POST', `/api/chapters/${ch.num}/resume`);
      addToast($t('writing.toasts.resumeStarted', { num: ch.num }), 'info');
    } catch (e) { addToast(e.message, 'error'); }
  }
  async function doFactCheckRerun() {
    if (!ch) return;
    try {
      await api('POST', `/api/chapters/${ch.num}/factcheck`);
      addToast($t('writing.toasts.factcheckStarted', { num: ch.num }), 'info');
    } catch (e) { addToast(e.message, 'error'); }
  }

  // "Actualizar interfaz": transient action that re-derives the chapter's
  // editor bookkeeping server-side (blocks, scene/trope chips, critic marks,
  // key-ffact associations) WITHOUT touching the prose. Chapters generated
  // before the chip/highlight features existed carry stale metadata; this
  // resyncs them in place so the new UI works on already-written chapters
  // instead of forcing a full rewrite.
  let resyncing = false;
  async function doResyncUI() {
    if (!ch || resyncing) return;
    resyncing = true;
    const num = ch.num;
    try {
      const res = await api('POST', `/api/chapters/${num}/resync-ui`);
      // Apply the chapter payload the endpoint already returns: the fresh
      // blocks carry the re-tiled scene/subscene metadata, so chips and inline
      // critic marks materialize immediately — no second GET racing the SSE
      // progress update (that race used to require closing/reopening the
      // chapter before the new interface showed up).
      if (res?.chapter && loadedNum === num) applyChapter(num, res.chapter);
      else await reloadChapterFull();
      if (res?.progress) progress.set(res.progress);
      else progress.set(await api('GET', '/api/progress'));
      // Force reactive rebuilds even if the block array reference happened to
      // compare equal: sheetBlocks re-derives from the fresh metadata and the
      // keyed-each re-renders every paragraph (chips + colored marks).
      lastChapterBlocksRef = null;
      ivRev++;
      sheetDirty = false;
      // The resync re-tiled the subscene paragraphs, so the locator window
      // offsets are stale — refetch and bump ivRev again when it lands so the
      // inline marks re-anchor to the fresh beat boundaries immediately.
      ivLocator = null; ivLocatorNum = -1;
      loadIVLocator(num).then(() => { if (loadedNum === num) ivRev++; });
      // Backend returns { status, scenes, realigned, chapter, progress } —
      // "scenes" is the number of scene blocks re-tiled. Fall back defensively
      // so the toast never claims "0 blocks" when the resync actually worked.
      const n = res?.scenes ?? res?.blocks ?? (ch?.blocks?.length ?? 0);
      addToast($t('writing.resync.done', { n }), 'success');
    } catch (e) { addToast(e.message, 'error'); }
    finally { resyncing = false; }
  }
  // Transitional one-shot action for chapters written BEFORE the subscene/beat
  // tiling existed: their scenes carry no beat units, so critic marks can only
  // ever cover a whole scene. This calls POST /subdivide-beats (pure structural
  // segmentation, no LLM, prose untouched), then behaves like a normal Refresh
  // UI so chips + inline marks land on the targeted beat right away.
  let subdividing = false;
  async function doSubdivideBeats() {
    if (!ch || subdividing) return;
    subdividing = true;
    const num = ch.num;
    try {
      const res = await api('POST', `/api/chapters/${num}/subdivide-beats`);
      if (res?.chapter && loadedNum === num) applyChapter(num, res.chapter);
      else await reloadChapterFull();
      if (res?.progress) progress.set(res.progress);
      else progress.set(await api('GET', '/api/progress'));
      lastChapterBlocksRef = null;
      ivRev++;
      sheetDirty = false;
      ivLocator = null; ivLocatorNum = -1;
      loadIVLocator(num).then(() => { if (loadedNum === num) ivRev++; });
      addToast($t('writing.subdivide.done', { n: res?.subdivided ?? 0, u: res?.subscenes ?? 0 }), 'success');
    } catch (e) { addToast(e.message, 'error'); }
    finally { subdividing = false; }
  }

  function doResetChapter() {
    if (!ch) return;
    const num = ch.num;
    confirmModal.set({
      message: $t('writing.chapter.resetAsk', { num }),
      onConfirm: async () => {
        try {
          progress.set(await api('POST', `/api/chapters/${num}/reset`));
          await loadChapterContent?.(num);
          addToast($t('writing.toasts.resetDone', { num }), 'success');
        } catch (e) { addToast(e.message, 'error'); }
      },
    });
  }

  async function copyContent() {
    if (!chapterContent) return;
    try {
      await navigator.clipboard.writeText(chapterContent);
      addToast($t('writing.toasts.copied'), 'success');
    } catch (e) { addToast($t('common.copy.failed'), 'error'); }
  }

  async function exportBook() {
    const written = chapters.filter(c => c.content_rev);
    if (written.length === 0) { addToast($t('writing.toasts.exportEmpty'), 'error'); return; }
    try {
      const r = await apiFetch('/api/export/txt');
      const blob = await r.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `${$config?.story?.title || p.title || $t('writing.export.defaultName')}.txt`;
      a.click();
      URL.revokeObjectURL(url);
      addToast($t('writing.toasts.exportDone', { n: written.length }), 'success');
    } catch (e) { addToast(e.message, 'error'); }
  }

  function prevChapter() { if ($selectedChapter > 0) selectChapter($selectedChapter - 1); }
  function nextChapter() { if ($selectedChapter < chapters.length - 1) selectChapter($selectedChapter + 1); }

  function smoothTransitions() {
    confirmModal.set({
      message: $t('writing.toasts.smoothAsk'),
      onConfirm: async () => {
        try {
          await api('POST', '/api/chapters/smooth-transitions');
          addToast($t('writing.toasts.smoothStarted'), 'info');
        } catch (e) { addToast(e.message, 'error'); }
      },
    });
  }
  async function setBookCompleted(completed) {
    const active = foreshadows.filter(f => f.status !== "resolved" && f.status !== "abandoned").length;
    const run = async () => {
      try {
        const suffix = completed && active ? "?confirm_foreshadows=true" : "";
        progress.set(await api("POST", completed ? "/api/story/complete" + suffix : "/api/story/resume"));
        addToast($t(completed ? "writing.book.completed" : "writing.book.resumed"), "success");
      } catch (e) { addToast(e.message, "error"); }
    };
    if (completed && active) {
      confirmModal.set({ message: $t("writing.book.foreshadowConfirm", { n: active }), onConfirm: run });
    } else {
      await run();
    }
  }
</script>

{#if !inWriting}
  <div id="WritingNotReadyState" class="text-center py-16 text-base-content/65">
    <div class="text-5xl mb-4"></div>
    <p class="text-base mb-1">{$t('writing.notReady.title')}</p>
    <p class="text-sm text-base-content/65 mb-6">{$t('writing.notReady.hint')}</p>
    <button class="btn btn-primary btn-sm" on:click={() => window.location.hash = '#outline'}>{$t('writing.notReady.goto')}</button>
  </div>
{:else}
  <div id="WritingPageContainer" class="space-y-4 mx-auto w-full" style="max-width:50rem;">
    {#if p.book_status !== 'completed' && (p.outline_batches || []).some(b => b.planned_final && chapters.find(c => c.num === b.end_ch)?.status === 'accepted')}
      <p class="alert alert-info">{$t('ending.completeHint')}</p>
    {/if}

    <!-- Progress -->
    <div id="CardSection_BookProgress" class="card bg-base-200">
      <div class="card-body p-4 gap-2">
        <div class="flex items-center gap-3">
          <h2 class="card-title text-base flex-1">{$t('writing.progress.title')}</h2>
          <label class="flex items-center gap-1.5 cursor-pointer" title={$t('writing.progress.autoConfirmTip')}>
            <input type="checkbox" class="toggle toggle-xs toggle-success" checked={$autoConfirm} on:change={toggleAutoConfirm} />
            <span class="text-xs text-base-content/60">{$t('writing.progress.autoConfirm')}</span>
          </label>
          <span class="text-xs text-base-content/65">{$t('writing.progress.totalWords', { n: totalWords.toLocaleString() })}</span>
          {#if accepted >= 2}
            <button class="btn btn-outline btn-xs" on:click={smoothTransitions} disabled={$taskRunning} title={$t('writing.btn.smoothTransitions.tip')}>{$t('writing.btn.smoothTransitions')}</button>
          {/if}
          <button class="btn btn-outline btn-xs" on:click={exportBook}>{$t('writing.btn.exportTxt')}</button>
			{#if p.book_status === 'completed'}
				<button class="btn btn-warning btn-xs" on:click={() => setBookCompleted(false)} disabled={$taskRunning}>{$t('writing.book.resume')}</button>
			{:else}
				<button class="btn btn-success btn-xs" on:click={() => setBookCompleted(true)} disabled={$taskRunning}>{$t('writing.book.complete')}</button>
			{/if}
        </div>
        <progress class="progress progress-primary w-full" value={pct} max="100"></progress>
        <div class="text-sm text-base-content/65">{$t('writing.progress.acceptedSummary', { pct, accepted, total })}</div>
      </div>
    </div>

    {#if writingConflict}
      <div id="CardSection_WritingConflict" class="card bg-error/10 border border-error/30 ">
        <div class="card-body p-4 gap-3">
          <h3 class="font-semibold text-error">{$t('writing.conflict.title')}</h3>
          <p class="text-sm">{$t('writing.conflict.summary')}：{writingConflict.summary}</p>
          {#if writingConflict.issues?.length}
            <div class="text-xs text-base-content/70">
              <div class="font-medium mb-1">{$t('writing.conflict.issues')}</div>
              <ul class="list-disc list-inside space-y-0.5">
                {#each writingConflict.issues as issue}
                  <li>{issue}</li>
                {/each}
              </ul>
            </div>
          {/if}
          {#if writingConflict.last_failure}
            {@const lf = writingConflict.last_failure}
            <div class="alert alert-warning text-xs py-2">
              <div>
                <div class="font-semibold">
                  {$t('writing.conflict.lastFailure', { 0: $t(`writing.conflict.failureStage.${lf.stage}` || 'writing.conflict.failureStage.revise') })}
                </div>
                <div>{lf.reason}</div>
                <div class="opacity-70 mt-0.5">{$t('writing.conflict.retryAlways')}</div>
              </div>
            </div>
          {/if}
          {#if (writingConflict.revise_attempts || 0) + (writingConflict.surgical_attempts || 0) > 0}
            {@const st = writingConflict.last_surgical_target}
            <div class="text-[11px] opacity-60">
              {$t('writing.conflict.attempts', { 0: writingConflict.revise_attempts || 0, 1: writingConflict.surgical_attempts || 0 })}
              {#if (writingConflict.surgical_attempts || 0) > 0 && st && st.length === 2 && st[0] >= 0}
                <span class="ml-1">{$t('writing.conflict.lastSurgicalTarget', { 0: st[0] + 1, 1: st[1] + 1 })}</span>
              {/if}
            </div>
          {/if}
          <div class="flex flex-wrap gap-2">
            {#each (writingConflict.suggested_actions || []) as action}
              {#if action.id === 'surgical_trope'}
                <button class="btn btn-success btn-xs" disabled={$taskRunning} title={action.description} on:click={() => resolveWritingConflict('surgical_trope')}>{$t('writing.conflict.surgicalTrope')}</button>
              {:else if action.id === 'edit_outline'}
                <button class="btn btn-warning btn-xs" disabled={$taskRunning} on:click={gotoOutlineForConflict}>{$t('writing.conflict.gotoOutline')}</button>
              {:else if action.id === 'adjust_foreshadow'}
                <button class="btn btn-warning btn-xs" disabled={$taskRunning} on:click={gotoForeshadows}>{$t('writing.conflict.gotoForeshadows')}</button>
              {:else if action.id === 'retry'}
                <button class="btn btn-primary btn-xs" disabled={$taskRunning} on:click={() => resolveWritingConflict('retry')}>{$t('writing.conflict.retry')}</button>
              {:else if action.id === 'revise_from_feedback'}
                <button class="btn btn-primary btn-xs" disabled={$taskRunning} on:click={() => resolveWritingConflict('revise_from_feedback')}>{$t('writing.conflict.reviseFromFeedback')}</button>
              {:else if action.id === 'force_review'}
                <button class="btn btn-outline btn-xs" disabled={$taskRunning} on:click={() => resolveWritingConflict('force_review')}>{$t('writing.conflict.forceReview')}</button>
              {/if}
            {/each}
            <button class="btn btn-ghost btn-xs" disabled={$taskRunning} on:click={() => resolveWritingConflict('dismiss')}>{$t('writing.conflict.dismiss')}</button>
          </div>
        </div>
      </div>
    {:else if orphanWriting}
      <div id="CardSection_OrphanChapter" class="card bg-warning/10 border border-warning/30 ">
        <div class="card-body p-4 gap-3">
          <h3 class="font-semibold text-warning">{$t('writing.orphan.title')}</h3>
          <p class="text-sm text-base-content/70">{$t('writing.orphan.hint')}</p>
          <div class="flex flex-wrap gap-2">
            <button class="btn btn-primary btn-xs" disabled={$taskRunning} on:click={doGenerate}>{$t('writing.orphan.retry')}</button>
            <button class="btn btn-outline btn-xs" disabled={$taskRunning} on:click={() => resolveWritingConflict('force_review')}>{$t('writing.orphan.forceReview')}</button>
            <button class="btn btn-warning btn-xs" disabled={$taskRunning} on:click={gotoOutlineForConflict}>{$t('writing.conflict.gotoOutline')}</button>
            <button class="btn btn-warning btn-xs" disabled={$taskRunning} on:click={gotoForeshadows}>{$t('writing.conflict.gotoForeshadows')}</button>
          </div>
        </div>
      </div>
    {/if}

    <!-- Row: chapter index (left) + foreshadow tracker (right; this is where
         the old "Scene plan" access card used to sit) -->
    <div id="WritingMetaRow" class="grid grid-cols-[minmax(0,1fr)_345px] gap-3 items-start">
      <div id="ChapterSidebarList" class="card bg-base-200 overflow-y-auto max-h-[calc(100vh-280px)] min-w-0">
        <ul class="menu menu-sm p-0 w-full">
          {#each chapters as c, i}
            <li>
              <button class="flex gap-2 items-center {$selectedChapter === i ? 'active' : ''}" on:click={() => selectChapter(i)}>
                <span class="w-2 h-2 rounded-full shrink-0 {statusMeta[c.status]?.dot || ''}"></span>
                <span class="text-base-content/65 w-6 shrink-0 text-right">{c.num}</span>
                <span class="flex-1 text-left truncate text-sm">{c.title}</span>
                {#if c.draft_saved_at && c.status !== 'accepted'}
                  <span class="badge badge-warning badge-outline badge-xs shrink-0" title={$t('writing.chapter.draftBadgeTip')}>{$t('writing.chapter.draftBadge')}</span>
                {/if}
                {#if i === currentIdx && c.status !== 'accepted'}
                  <span class="badge badge-primary badge-xs shrink-0">{$t('writing.tag.current')}</span>
                {/if}
              </button>
            </li>
            <!-- Act-critique "steps": when the pre-plan switches from one act
                 to the next right after this chapter (or, for the last act,
                 after the final chapter), offer its manual critique inline —
                 these used to be an isolated button row below the tabs. -->
            {#each actsEndingAfter(c.num) as act (act.no)}
              <li>
                <button class="flex gap-2 items-center border-t border-base-content/10"
                        disabled={$taskRunning || !!p?.act_analyses?.[act.no]}
                        title={$t('act.analysis.manualBtnTip', { start: act.start_ch, end: act.end_ch })}
                        on:click={() => critiqueAct(act.no)}>
                  <span class="shrink-0 text-warning text-xs">✦</span>
                  <span class="flex-1 text-left text-xs opacity-80">{$t('act.analysis.manualBtn', { no: act.no, name: act.name || '' })}</span>
                  {#if p?.act_analyses?.[act.no]}
                    <span class="badge badge-success badge-xs shrink-0">✓</span>
                  {/if}
                </button>
              </li>
            {/each}
          {/each}
        </ul>
      </div>

      <div id="CardSection_ForeshadowTracker" class="card bg-base-200 min-w-0">
        {#if foreshadows.length > 0}
          <div class="card-body p-4 gap-2">
            <div class="flex items-center justify-between gap-2">
              <h3 class="font-medium text-sm">{$t('writing.fs.title')}</h3>
              <button class="btn btn-outline btn-xs" on:click={() => window.location.hash = '#foreshadows'}>{$t('writing.fs.goto')}</button>
            </div>
            <div class="flex flex-wrap gap-2 text-xs">
              <span class="badge badge-ghost">{$t('writing.fs.total', { n: foreshadows.length })}</span>
              <span class="badge badge-info badge-outline">{$t('writing.fs.active', { n: fsActive.length })}</span>
              {#if fsOverdue.length > 0}
                <span class="badge badge-error">{$t('writing.fs.overdue', { n: fsOverdue.length })}</span>
              {/if}
              {#if fsNearTarget.length > 0}
                <span class="badge badge-warning badge-outline">{$t('writing.fs.nearTarget', { n: fsNearTarget.length })}</span>
              {/if}
            </div>
            {#if fsOverdue.length > 0}
              <p class="text-xs text-warning">{$t('writing.fs.overdueDetail', { names: fsOverdue.map(f => `#${f.id} ${f.name}`).join(', ') })}</p>
            {:else if fsNearTarget.length > 0}
              <p class="text-xs text-base-content/65">{$t('writing.fs.nearDetail', { names: fsNearTarget.map(f => f.name).join(', ') })}</p>
            {/if}
          </div>
        {:else}
          <div class="card-body p-4 flex items-center justify-between gap-2">
            <p class="text-sm text-base-content/65">{$t('writing.fs.none')}</p>
            <button class="btn btn-outline btn-xs" on:click={() => window.location.hash = '#foreshadows'}>{$t('writing.fs.setup')}</button>
          </div>
        {/if}
      </div>
    </div><!-- /WritingMetaRow -->

    <!-- CHAPTER TITLE: big heading above the stepper tabs so the user always
         knows which chapter the three steps refer to. -->
    <div id="ChapterTitleHeader" class="pt-12 pb-3 flex items-center gap-3 flex-wrap">
      <h1 class="text-3xl font-bold leading-tight truncate max-w-full">
        {#if ch}{ch.title}{:else}{$t('writing.emptySelection')}{/if}
      </h1>
      {#if ch}
        <span class="badge badge-sm shrink-0 {statusMeta[ch.status]?.cls || 'badge-ghost'}">{statusMeta[ch.status]?.label || ch.status}</span>
      {/if}
    </div>

    <!-- STEP TABS: three horizontal step tabs below WritingMetaRow. The
         active one gets a white card + dark numbered badge; earlier steps
         show a check and fade slightly; future steps are dimmed until
         hover. A progress bar attached at the bottom reflects the current
         step (1/3 → 3/3). Panes: 1 = Scene plan (+ scene-plan critic),
         2 = WritingWorkspace (+ chapter critic), 3 = consistency facts +
         accept-chapter button. -->
    <div id="WritingStepTabs" class="pt-1">
      <div class="grid grid-cols-3 gap-2 py-1">
        {#each writingTabDefs as def (def.num)}
          {@const isCurrent = writingTab === def.num}
          {@const isPast = writingTab > def.num}
          {@const stepDoneIcon = '<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6 9 17l-5-5"/></svg>'}
          <button
            type="button"
            id={'step-tab-' + def.num}
            on:click={() => setWritingTab(def.num)}
            class="flex flex-col p-2.5 rounded-lg text-left group focus:outline-none step-transition relative z-10
              {isCurrent ? 'bg-white border border-neutral-200 shadow-subtle' : isPast ? 'bg-white/40 border border-transparent opacity-70 hover:opacity-100' : 'bg-transparent border border-transparent opacity-40 hover:opacity-100'}"
          >
            <div class="flex items-center justify-between mb-1.5">
              <span
                id={'step-icon-' + def.num}
                class="w-4 h-4 rounded flex items-center justify-center text-[10px] font-bold step-transition
                  {isCurrent || isPast ? 'bg-neutral-900 text-white' : 'bg-neutral-200 text-neutral-500'}"
              >{#if isPast}{@html stepDoneIcon}{:else}{def.num}{/if}</span>
              <span id={'step-sub-' + def.num} class="text-[9px] font-mono text-neutral-400">0{def.num}</span>
            </div>
            <span
              id={'step-title-' + def.num}
              class="text-xs truncate {isCurrent ? 'font-semibold text-neutral-900' : isPast ? 'font-medium text-neutral-600' : 'font-medium text-neutral-500'}"
            >{$t(def.titleKey)}</span>
          </button>
        {/each}
      </div>
      <!-- PROGRESS BAR ATTACHED AT THE BOTTOM -->
      <div class="relative w-full h-1 bg-neutral-100 rounded-full overflow-hidden mt-1">
        <div
          id="writing-step-progress-bar"
          class="absolute top-0 left-0 h-full bg-neutral-900 step-transition"
          style="width: {writingTab * 33.33}%;"
        ></div>
      </div>
    </div><!-- /WritingStepTabs -->

    <!-- ═════════ TAB 1 · SCENE MATRIX ═════════
         The scene-plan beat-sheet editor INLINE (it used to live in a modal
         opened from an access card) + the bestseller critic over the PENDING
         scene plans. -->
    {#if writingTab === 1}
      <div class="mt-3 min-w-0">
        {#key scenePlanEditorNum}
          <ScenePlanEditor chapterNum={scenePlanEditorNum}
            onGenerated={() => setWritingTab(2)} />
        {/key}
      </div>

      <!-- Bestseller critic over the PENDING scene plans (beat sheets): same
           card as the outline analysis, but the editor knows it is reviewing
           scene plans, not prose. Scene-level replacements are proposed per
           chapter and applied back to each plan (re-opening its review gate). -->
      <div id="CardSection_ScenePlanAnalysis" class="mt-3 space-y-1">
        <BestsellerCritiqueCard
          kind="sceneplan"
          report={scenePlanAnalysis}
          disabled={$taskRunning}
          hasTargets={scenePlanPendingCount > 0}
          scrollId="CardSection_ScenePlanAnalysis" />
      </div>
    {/if}

    <!-- ═════════ SHARED BELOW-TABS AREA (tabs 2 & 3) ═════════
         Act critic reports stay visible on both the writing and the review
         tabs. The manual act-critique buttons now live INSIDE the chapter
         index (ChapterSidebarList) as extra "steps" placed where each act
         ends; the foreshadow tracker card moved next to the chapter index. -->
    {#if writingTab === 2 || writingTab === 3}
    <!-- Bestseller critic over FINISHED ACTS of the pre-planning: one report
         per act, generated automatically right after the pipeline confirms
         the act's last chapter (and manually from the chapter-index rows).
         Suggestions carry small trope-unit interventions that
         rewriteSceneTrope repairs surgically. -->
    {#each actAnalyses as a (a.no)}
      <div id="CardSection_ActAnalysis_{a.no}" class="mt-3 space-y-1">
        <BestsellerCritiqueCard
          kind="act"
          report={a.rep}
          actNo={a.no}
          disabled={$taskRunning}
          hasTargets={true}
          scrollId="CardSection_ActAnalysis_" + a.no />
      </div>
    {/each}

    {/if}

    <!-- ═════════ TAB 2 · CHAPTER WRITING ═════════
         The whole WritingWorkspace (reader card + sticky save bar) lives in
         this pane; the bestseller critic over the drafted chapter sits at
         the bottom of it. -->
    {#if writingTab === 2}
    <!-- Chapter area -->
    <div id="WritingWorkspace" style="min-height:400px">
      <!-- Content area -->
      <div id="ChapterContentArea" class="min-w-0 w-full">
        {#if ch}
          <div id="CardSection_ChapterReader" class="card bg-base-200" use:setReaderCard>
            <div class="card-body p-4 gap-2">
              <div class="chapter-head-row flex items-center gap-2 flex-wrap">
                <h2 class="card-title text-base flex-1 min-w-0">{$t('writing.chapter.title', { num: ch.num, title: ch.title })}</h2>
                <span class="badge badge-sm {statusMeta[ch.status]?.cls || 'badge-ghost'}">{statusMeta[ch.status]?.label || ch.status}</span>
                {#if showTaskTokens}
                  <TaskTokenBadge className="text-xs text-base-content/65 font-mono" />
                {:else if chapterWordCount > 0}
                  <span class="text-xs text-base-content/65">{$t('writing.chapter.words', { n: chapterWordCount.toLocaleString() })}</span>
                {/if}
              </div>

              {#if ch.outline}
                <details class="chapter-outline-fold bg-base-100">
                  <summary class="p-2 text-xs text-base-content/65 cursor-pointer select-none">{$t('writing.chapter.outline')}</summary>
                  <div class="px-2 pb-2 text-sm text-base-content/70">{ch.outline}</div>
                </details>
              {/if}

              <!-- The chapter summary moved to the "Review & accept" tab. -->

              {#if displayContent}
                {#if isStreamingThis}
                  <div class="text-xs text-warning/80 flex items-center gap-1.5">
                    <span class="loading loading-dots loading-xs"></span>
                    {$t('writing.chapter.streamHint')}
                  </div>
                {/if}
                <!-- svelte-ignore a11y-no-static-element-interactions -->
                <div id="ChapterProseViewer" bind:this={contentEl} class="rounded-lg p-4 text-[15px] chapter-content min-h-[200px]"
                     on:mouseup={checkContentSelection}
                     on:scroll={hideQuotePopover}>
                  {#if isStreamingThis}
                    {displayContent}
                    <span class="inline-block w-2 h-4 bg-primary/70 animate-pulse ml-0.5 align-text-bottom"></span>
                  {:else if chapterBlocks.length > 0}
                    {@const sheetMeta = blockMetaById()}
                    <!-- Continuous Word-like sheet: one always-editable surface;
                         scene/trope chips sit between paragraphs and are NOT
                         text-selectable (user-select:none + contenteditable=false). -->
                    <div id="ChapterSheetEditor" class="space-y-1" bind:this={sheetEl}
                         use:sheetEditable
                         contenteditable={!$taskRunning && editingBlockId == null ? 'true' : 'false'}
                         spellcheck="false"
                         on:input={() => { serializeSheet(); }}
                         on:keyup={checkSheetSelection}
                         on:mouseup={checkSheetSelection}
                         on:focusin={() => { sheetFocused = true; }}
                         on:focusout={(e) => { if (!e.currentTarget.contains(e.relatedTarget)) { sheetFocused = false; hideSelMenu(); } }}
    on:click={onSheetClick}
                         on:scroll={hideSelMenu}>
                      {#each chapterBlocks as b, bi (b.id + ':' + ivRev)}
                        {@const ms = markersBefore(chapterBlocks, bi, sheetMeta)}
                        <!-- Key facts referenced by the blocks of the trope unit this
                             boundary opens (falling back to the whole scene when the
                             block has no unit index): shown as compact "#N" chips on
                             their own row UNDER the subscene chip — facts travel WITH
                             the trope markers, never on the scene chip. Clicking one
                             toggles an inline highlighter mark over the exact sentence
                             the fact refers to (no whole-block tint). -->
                        {@const chipFactsRow = (() => {
                          if (ms.length === 0) return [];
                          const cur = sheetMeta.get(b.id);
                          if (!cur || cur.scene_pos == null || cur.scene_pos < 0) return [];
                          const t0 = cur.trope_index != null ? cur.trope_index - 1 : null;
                          const unitBlocks = chapterBlocks.filter(x => {
                            if (x.scene_pos !== cur.scene_pos) return false;
                            if (t0 == null || !x.trope_count || x.trope_count <= 1) return true;
                            const xt = x.trope_index != null ? x.trope_index - 1 : null;
                            return xt == null || xt === t0;
                          });
                          const ids = new Set();
                          const out = [];
                          for (const ub of unitBlocks) {
                            for (const f of blockFacts(ub.id)) {
                              if (!ids.has(f.id)) { ids.add(f.id); out.push({ fact: f, bid: ub.id }); }
                            }
                          }
                          return out.slice(0, 6);
                        })()}
                        {#if ms.length > 0}
                          <!-- Scene and subscene chips are NEVER merged: each
                               marker renders on its OWN centered row — the
                               scene chip on top, the subscene chip below it. -->
                          {#each ms as m, mi (m.kind + ':' + m.pos + ':' + (m.idx ?? 0))}
                            <div class="sheet-chip-row group/chiprow relative flex flex-wrap items-center justify-center gap-1.5 py-0.5" data-bid={mi === 0 ? (-bi - 1) : (-bi - 100000 - mi)}>
                              <!-- SCENE chips are ALWAYS fully visible (they own
                                   the description tooltip). Subscene chips stay
                                   subtle until hovered. Only SCENE chips carry the
                                   "i" tooltip; subscene chips never do. -->
                              <span class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 chip-scene-or-trope {m.kind === 'scene' ? 'chip-always-on' : ''} bg-base-content/5 border border-base-content/15 {m.kind === 'scene' ? 'cursor-help' : 'cursor-default'} select-none" contenteditable="false">
                                <span class="sheet-chip-text text-[9px] font-semibold tracking-wider uppercase text-base-content/50 select-none">
                                  {#if m.kind === 'scene'}{$t('writing.blockSceneMarker', [m.pos])}{:else}{$t('writing.tropeMarkerChip', [m.label || ('#' + m.idx), m.idx, m.count])}{/if}
                                </span>
                              </span>
                              {#if m.kind === 'scene' && (m.title || m.desc)}
                                <!-- Scene-only affordance: an "i" icon OUTSIDE the
                                     chip that opens the same black popover used by
                                     the SmartSelect tips, with the scene's title
                                     and description. Subscene chips have NO
                                     tooltip at all. -->
                                <InfoTip text={(m.title ? m.title + '\n\n' : '') + (m.desc || $t('writing.sceneDescEmpty'))} label={$t('writing.sceneDescTipLabel')}>
                                  <span slot="trigger" class="scene-desc-icon" role="presentation">i</span>
                                </InfoTip>
                              {/if}
                              {#if m.kind === 'trope'}
                                <!-- Hover action lives OUTSIDE the chip AND outside
                                     the layout flow: an absolutely-positioned ghost
                                     pill anchored to the row's right edge. It takes
                                     ZERO width in the flex row, so the centered chip
                                     never shifts left/right when the pill appears.
                                     Only SUBSCENE rows get it — scene chips don't. -->
                                <button type="button" contenteditable="false"
                                        class="chip-add-before chip-add-before--ghost focus-visible:opacity-100 inline-flex items-center rounded-full px-1.5 py-0.5 text-[9px] font-semibold uppercase tracking-wider bg-primary/10 text-primary hover:bg-primary/20"
                                        on:click|stopPropagation|preventDefault={() => startBlockInsertBefore(b.id)}>＋ {$t('writing.block.insertBefore')}</button>
                              {/if}
                            </div>
                          {/each}
                          {#if chipFactsRow}
                            <!-- Key-fact companion row under the subscene chip:
                                 facts travel WITH the trope markers, never on the
                                 scene chip. Clicking one toggles an inline
                                 highlighter mark over the exact sentence the fact
                                 refers to (no whole-block tint). -->
                            <div class="sheet-chip-row flex flex-wrap items-center justify-center gap-1 py-0.5" data-bid={-bi - 200000}>
                              <span class="flex items-center gap-0.5 select-none" contenteditable="false">
                                {#each chipFactsRow as cf (cf.fact.id)}
                                  <button type="button" class="keyfact-chip" contenteditable="false"
                                          title={$t('writing.keyFactChip.tip') + ': ' + (cf.fact.content || '')}
                                          on:click|stopPropagation|preventDefault={() => toggleFactHighlight(cf.fact, cf.bid)}>{$t('writing.keyFactChip')} #{cf.fact.id}</button>
                                {/each}
                              </span>
                            </div>
                          {/if}
                        {/if}
                        <div id={'story-block-' + b.id} class="sheet-para relative rounded -mx-2 px-2 py-1 leading-relaxed whitespace-pre-wrap {selectedBlockId === b.id ? 'story-block-selected' : ''}"
                             data-bid={b.id}
                             contenteditable={editingBlockId === b.id ? 'false' : 'true'}>
                          {#if editingBlockId === b.id}
                            <textarea class="textarea textarea-sm w-full text-[15px] leading-relaxed" rows={Math.max(3, Math.ceil(b.text.length / 40))} bind:value={editingText} disabled={$taskRunning}></textarea>
                            <div class="flex gap-2 justify-end mt-1">
                              <button class="btn btn-ghost btn-xs" on:click={cancelBlockOps}>{$t('common.cancel')}</button>
                              <button class="btn btn-primary btn-xs" on:click={saveBlockEdit} disabled={$taskRunning || !editingText.trim()}>{$t('common.save')}</button>
                            </div>
                          {:else}
                            {#if blockFacts(b.id).length}<div class="flex flex-wrap gap-1 mb-1" contenteditable="false">{#each blockFacts(b.id) as fact}<button class="badge badge-warning badge-sm cursor-pointer" on:click|stopPropagation|preventDefault={() => toggleFactHighlight(fact, b.id)}>{$t('facts.marker')} #{fact.id}</button>{/each}</div>{/if}
                            {#if (() => { const ivs = ivSegmentsFor(b.text, b.id); const fms = factMarkSegments(b.text, b.id); return ivs || fms; })()}
                              {@const ivSegs = ivSegmentsFor(b.text, b.id)}
                              {@const fmSegs = factMarkSegments(b.text, b.id)}
                              <span class="whitespace-pre-wrap">{#each (fmSegs && !ivSegs ? fmSegs : (ivSegs || [{ t: b.text }])) as seg, si (si)}{#if seg.iv}{@const segIvs = findIvsForKeys(seg.ivKeys || [seg.ivKey], seg.t)}<mark class="iv-mark" style={ivMarkStyleMulti(segIvs.map(x => x.key))} role="button" tabindex="-1" title={$t('writing.iv.popupTitle')} on:click|stopPropagation|preventDefault={(e) => openIVPopup(e, segIvs)} on:mousedown|stopPropagation={(e) => e.preventDefault()} on:keydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); openIVPopup(e, segIvs); } }}>{seg.t}</mark>{#if segIvs.length > 1}<sup class="iv-count" style={'color:' + ivColor(segIvs[0].key)}>×{segIvs.length}</sup>{/if}{:else if seg.fact}<mark class="fact-mark">{seg.t}</mark>{:else}{seg.t}{/if}{/each}</span>
                            {:else}
                              <span class="whitespace-pre-wrap">{b.text}</span>
                            {/if}
                            {#if tropeIssuesFor(b)}
                              {@const ti = tropeIssuesFor(b)}
                              <span class="inline-flex items-center gap-1 align-top ml-1" contenteditable="false">
                                {#if ti.fact?.length}
                                  <InfoTip text={tropeIssueTipText(ti, 'fact')} label={$t('writing.tropeDots.factTitle')}>
                                    <span slot="trigger" class="trope-dot trope-dot-fact" aria-label={$t('writing.tropeDots.factTitle')}></span>
                                  </InfoTip>
                                {/if}
                                {#if ti.critic?.length}
                                  <InfoTip text={tropeIssueTipText(ti, 'critic')} label={$t('writing.tropeDots.criticTitle')}>
                                    <span slot="trigger" class="trope-dot trope-dot-critic" aria-label={$t('writing.tropeDots.criticTitle')}></span>
                                  </InfoTip>
                                {/if}
                              </span>
                            {/if}
                          {/if}
                          {#if revisingBlockId === b.id}
                            <div class="bg-base-100 rounded p-2 mt-1 space-y-1" contenteditable="false">
                              <textarea class="textarea textarea-sm w-full text-sm" bind:value={blockFeedback} placeholder={$t('writing.block.revisePlaceholder')} disabled={$taskRunning}></textarea>
                              <div class="flex gap-2 justify-end">
                                <button class="btn btn-ghost btn-xs" on:click={cancelBlockOps}>{$t('common.cancel')}</button>
                                <button class="btn btn-primary btn-xs" on:click={submitBlockRevise} disabled={$taskRunning || !blockFeedback.trim()}>{$t('writing.block.reviseSubmit')}</button>
                              </div>
                            </div>
                          {/if}
                          {#if insertAfterId === b.id}
                            <div class="bg-base-100 rounded p-2 mt-1 space-y-1" contenteditable="false">
                              <textarea class="textarea textarea-sm w-full text-sm" bind:value={insertText} placeholder={$t('writing.block.insertPlaceholder')} disabled={$taskRunning}></textarea>
                              <div class="flex gap-2 justify-end">
                                <button class="btn btn-ghost btn-xs" on:click={cancelBlockOps}>{$t('common.cancel')}</button>
                                <button class="btn btn-primary btn-xs" on:click={saveBlockInsert} disabled={$taskRunning || !insertText.trim()}>{$t('writing.block.insertSubmit')}</button>
                              </div>
                            </div>
                          {/if}
                        </div>
                      {/each}
                      <!-- "End of chapter" chip: same affordance as the subscene
                           chips, but anchored to the END of the chapter. The action
                           label says "add subscene before" because the new block is
                           inserted BEFORE this end-of-chapter marker (i.e. right
                           after the last written paragraph). -->
                      {#if chapterBlocks.length > 0}
                        {@const lastBid = chapterBlocks[chapterBlocks.length - 1].id}
                        <div class="sheet-chip-row group/chiprow relative flex flex-wrap items-center justify-center gap-1.5 py-0.5" data-bid={-999998}>
                          <span class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 chip-scene-or-trope bg-base-content/5 border border-base-content/15 cursor-default select-none" contenteditable="false">
                            <span class="sheet-chip-text text-[9px] font-semibold tracking-wider uppercase text-base-content/50 select-none" title={$t('writing.block.endOfChapterTip')}>⬛ {$t('writing.block.endOfChapter')}</span>
                          </span>
                          <!-- Action pill lives OUTSIDE the chip AND outside the
                               layout flow (absolute ghost at the row's right edge):
                               zero width in the flex row, so the centered chip never
                               shifts. Label says "add subscene before" because the
                               new block lands BEFORE this end-of-chapter marker. -->
                          <button type="button" contenteditable="false"
                                  class="chip-add-before chip-add-before--ghost focus-visible:opacity-100 inline-flex items-center rounded-full px-1.5 py-0.5 text-[9px] font-semibold uppercase tracking-wider bg-primary/10 text-primary hover:bg-primary/20"
                                  on:click|stopPropagation|preventDefault={() => startBlockInsert(lastBid)}>＋ {$t('writing.block.insertBefore')}</button>
                          {#if insertAfterId === lastBid}
                            <div class="w-full bg-base-100 rounded p-2 mt-1 space-y-1 text-left" contenteditable="false">
                              <textarea class="textarea textarea-sm w-full text-sm" bind:value={insertText} placeholder={$t('writing.block.insertPlaceholder')} disabled={$taskRunning}></textarea>
                              <div class="flex gap-2 justify-end">
                                <button class="btn btn-ghost btn-xs" on:click={cancelBlockOps}>{$t('common.cancel')}</button>
                                <button class="btn btn-primary btn-xs" on:click={saveBlockInsert} disabled={$taskRunning || !insertText.trim()}>{$t('writing.block.insertSubmit')}</button>
                              </div>
                            </div>
                          {/if}
                        </div>
                      {/if}
                    </div>
                  {:else}
                    {displayContent}
                  {/if}
                </div>
                {#if quotePopover}
                  <button type="button"
                    class="fixed z-50 btn btn-primary btn-xs"
                    style="left: {quotePopover.x}px; top: {quotePopover.y}px; transform: translate(-50%, -100%); margin-top: -6px;"
                    on:click={insertQuoteToFeedback}
                    title={$t('writing.revise.quoteBtn.tip')}>
                    {$t('writing.revise.quoteBtn')}
                  </button>
                {/if}
                {#if selMenu && editingBlockId == null}
                  <div class="fixed z-50 flex gap-1 bg-base-200 border border-base-content/20 rounded-lg px-1.5 py-1 shadow-lg"
                       style="left: {selMenu.x}px; top: {selMenu.y}px; transform: translate(-50%, -100%); margin-top: -6px;">
                    <button class="btn btn-outline btn-xs" disabled={$taskRunning} on:click|stopPropagation={() => selFromSheet('edit')}>{$t('writing.block.edit')}</button>
                    <button class="btn btn-outline btn-xs" disabled={$taskRunning} on:click|stopPropagation={() => selFromSheet('revise')}>{$t('writing.block.revise')}</button>
                    <button class="btn btn-outline btn-xs" disabled={$taskRunning} on:click|stopPropagation={() => selFromSheet('insertAfter')}>{$t('writing.block.insertAfter')}</button>
                    <button class="btn btn-error btn-outline btn-xs" disabled={$taskRunning} on:click|stopPropagation={() => selFromSheet('delete')}>{$t('writing.block.delete')}</button>
                  </div>
                {/if}
                {#if ivPopup}
                  {@const ivList = ivPopup.ivs && ivPopup.ivs.length ? ivPopup.ivs : [ivPopup.iv]}
                  {@const ivActive = ivList[ivPopup.active] || ivList[0]}
                  {@const ivC = ivColor(ivActive.key || ivActive)}
                  <div class="fixed z-50 rounded-lg p-3 shadow-xl"
                       use:popupMeasure
                       style={popupPos(ivPopup) + 'border:1px solid ' + ivC + ';box-shadow:0 8px 24px rgba(0,0,0,.28), inset 0 3px 0 0 ' + ivC +';background:#ffffff;color:#1f2937'}
                       role="dialog" aria-label={$t('writing.iv.popupTitle')}>
                    <div class="text-xs font-semibold flex items-center gap-1.5 mb-1" style={'color:' + ivC}>
                      <span class="inline-block w-2.5 h-2.5 rounded-full shrink-0" style={'background:' + ivC}></span>
                      {$t('writing.iv.popupTitle')}
                    </div>
                    {#if ivList.length > 1}
                      <!-- Multiple suggestions anchored on this same beat:
                           one tab per intervention, each keeping its own
                           inline color so Apply/Dismiss target exactly one. -->
                      <div class="flex flex-wrap gap-1 mb-2">
                        {#each ivList as tIv, ti}
                          <button class="btn btn-xs py-0 px-2 h-6 rounded-full border text-[10px] leading-none"
                                  style={ti === ivPopup.active
                                    ? ('background:' + ivColor(tIv.key) + ';color:#fff;border-color:' + ivColor(tIv.key))
                                    : ('border-color:' + ivColor(tIv.key) + ';color:' + ivColor(tIv.key) + ';background:transparent')}
                                  on:click|stopPropagation={() => { ivPopup = { ...ivPopup, active: ti, iv: ivList[ti] }; }}>
                            <span class="inline-block w-1.5 h-1.5 rounded-full mr-1" style={'background:' + (ti === ivPopup.active ? '#fff' : ivColor(tIv.key))}></span>
                            #{ti + 1} · {String(tIv.title || tIv.action || $t('writing.iv.popupTitle')).slice(0, 24)}
                          </button>
                        {/each}
                      </div>
                    {/if}
                    <div class="text-[11px] opacity-70">{@html ivActionLabelHtml(ivActive)}</div>
                    {#if ivActive.current_text}
                      <div class="bg-base-100 rounded p-2 mt-1 text-xs whitespace-pre-wrap overflow-hidden" style="max-height:96px">{ivActive.current_text}</div>
                    {:else}
                      <div class="text-xs italic opacity-60">{$t('writing.iv.stale')}</div>
                    {/if}
                    {#if ivActive.feedback}
                      <div class="text-xs leading-snug whitespace-pre-wrap overflow-hidden mt-1" style="max-height:80px">{ivActive.feedback}</div>
                    {/if}
                    <div class="flex gap-2 justify-between items-center mt-2">
                      <button class="btn btn-link btn-xs p-0 h-auto underline decoration-dotted" style={'color:' + ivC} on:click|stopPropagation={openIVDetail}>{$t('writing.iv.viewFull')}</button>
                      <div class="flex gap-2">
                        <button class="btn btn-ghost btn-xs" on:click={dismissPendingIV} disabled={$taskRunning}>{$t('writing.iv.dismiss')}</button>
                        <button class="btn btn-xs text-base-100 border-0" style={'background:' + ivC} on:click={() => applyPendingIV()} disabled={$taskRunning || applyingIV}>{$t('writing.iv.apply')}</button>
                      </div>
                    </div>
                  </div>
                {/if}
                {#if ivDetail}
                  <div class="fixed inset-0 z-[60] bg-black/50 flex items-center justify-center p-4" role="presentation"
                       on:click={hideIVPopup} on:keydown={(e) => e.key === 'Escape' && hideIVPopup()} tabIndex="-1">
                    <div class="card w-full max-w-2xl bg-base-200 shadow-2xl" style={'border:1px solid ' + ivDetail.color + ';border-top:4px solid ' + ivDetail.color}
                         role="dialog" aria-modal="true" aria-label={$t('writing.iv.popupTitle')}
                         on:click|stopPropagation on:keydown|stopPropagation>
                      <div class="card-body p-5 overflow-hidden">
                        <h3 class="card-title text-sm items-center gap-2" style={'color:' + ivDetail.color}>
                          <span class="inline-block w-3 h-3 rounded-full" style={'background:' + ivDetail.color}></span>
                          {$t('writing.iv.popupTitle')}
                          <button class="btn btn-circle btn-ghost btn-xs ml-auto" on:click={hideIVPopup} aria-label="close">✕</button>
                        </h3>
                        <div class="text-xs opacity-75">{@html ivActionLabelHtml(ivDetail.iv)}</div>
                        <div class="divider my-1"></div>
                        <div class="flex-1 min-h-0 overflow-y-auto space-y-3 text-sm" style="max-height:60vh">
                          {#if ivDetail.iv.title}
                            <div class="font-semibold">{ivDetail.iv.title}</div>
                          {/if}
                          {#if ivDetail.iv.feedback}
                            <div>
                              <div class="text-[11px] uppercase tracking-wide opacity-50 mb-1">{$t('writing.iv.feedbackLabel')}</div>
                              <div class="whitespace-pre-wrap leading-relaxed">{ivDetail.iv.feedback}</div>
                            </div>
                          {/if}
                          {#if ivDetail.iv.current_text}
                            <div>
                              <div class="text-[11px] uppercase tracking-wide opacity-50 mb-1">{$t('writing.iv.currentLabel')}</div>
                              <div class="bg-base-100 rounded p-3 text-xs whitespace-pre-wrap leading-relaxed" style={'border-left:3px solid ' + ivDetail.color}>{ivDetail.iv.current_text}</div>
                            </div>
                          {:else}
                            <div class="text-xs italic opacity-60">{$t('writing.iv.stale')}</div>
                          {/if}
                          {#if ivDetail.iv.replacement || ivDetail.iv.proposed_text}
                            <div>
                              <div class="text-[11px] uppercase tracking-wide opacity-50 mb-1">{$t('writing.iv.proposedLabel')}</div>
                              <div class="bg-base-100 rounded p-3 text-xs whitespace-pre-wrap leading-relaxed border-l-4 border-success">{ivDetail.iv.replacement || ivDetail.iv.proposed_text}</div>
                            </div>
                          {/if}
                          {#if ivDetail.iv.severity}
                            <div class="text-[11px] opacity-50">{$t('writing.iv.severityLabel')}: {ivDetail.iv.severity}</div>
                          {/if}
                        </div>
                        <div class="card-actions justify-end mt-2">
                          <button class="btn btn-ghost btn-sm" on:click={dismissPendingIV} disabled={$taskRunning}>{$t('writing.iv.dismiss')}</button>
                          <button class="btn btn-sm text-base-100 border-0" style={'background:' + ivDetail.color} on:click={() => applyPendingIV()} disabled={$taskRunning || applyingIV}>{$t('writing.iv.apply')}</button>
                        </div>
                      </div>
                    </div>
                  </div>
                {/if}
              {:else if ch.status === 'pending'}
                <div class="bg-base-100 rounded-lg p-6 text-center text-sm text-base-content/65">
                  {#if isCurrent}
                    {$t('writing.chapter.pendingCurrent')}
                  {:else}
                    {$t('writing.chapter.pendingOther', { n: chapters[currentIdx]?.num ?? '-' })}
                  {/if}
                </div>
              {/if}

              <!-- Actions -->
              <div id="ChapterActionsBar" class="flex gap-2 flex-wrap items-center mt-1">
                {#if ch.status === 'pending' && isCurrent}
                  <button class="btn btn-primary btn-sm" on:click={doGenerate} disabled={$taskRunning}>{$t('writing.btn.generate')}</button>
                  <!-- The scene-plan editor is inline in the "Scene matrix" tab now; this button just jumps there. -->
                  <button class="btn btn-outline btn-sm" on:click={() => setWritingTab(1)} disabled={$taskRunning} title={$t('writing.scenePlan.btnTip')}>{$t('writing.scenePlan.btn')}</button>
                {/if}
                {#if ch.draft_saved_at && !isStreamingThis}
                  <button class="btn btn-warning btn-sm" on:click={doResumeDraft} disabled={$taskRunning} title={$t('writing.chapter.resumeTip')}>{$t('writing.chapter.resume')}</button>
                  <button class="btn btn-error btn-outline btn-sm" on:click={doResetChapter} disabled={$taskRunning}>{$t('writing.chapter.reset')}</button>
                {/if}
                {#if ch.status === 'review' && isCurrent}
                  <button class="btn btn-success btn-sm" on:click={doConfirm} disabled={$taskRunning}>{$t('writing.btn.confirm')}</button>
                {/if}
                {#if hasContent && ch.status !== 'writing'}
                  <button class="btn btn-outline btn-sm" on:click={() => showRevise = !showRevise} disabled={$taskRunning}>{$t('writing.btn.revise')}</button>
                  <button class="btn btn-outline btn-sm" on:click={doFactCheckRerun} disabled={$taskRunning} title={$t('writing.chapter.factcheckTip')}>{$t('writing.chapter.factcheck')}</button>
                  <button class="btn btn-outline btn-sm" on:click={() => { showKnowledge = !showKnowledge; showRevise = true; if (!showKnowledge) selectedSettings = []; }} disabled={$taskRunning || savingKnowledge}>{$t('writing.knowledge.title')}</button>
                  {#if hasPolishSkills}
                    <button class="btn btn-outline btn-sm" on:click={doPolish} disabled={$taskRunning} title={$t('writing.btn.polish.tip')}>{$t('writing.btn.polish')}</button>
                  {/if}
                  <button class="btn btn-outline btn-sm" on:click={copyContent}>{$t('writing.btn.copy')}</button>
                  <button class="btn btn-outline btn-sm" on:click={doResyncUI} disabled={$taskRunning || resyncing} title={$t('writing.resync.tip')}>
                    {#if resyncing}<span class="loading loading-spinner loading-xs"></span>{/if}
                    {$t('writing.resync.btn')}
                  </button>
                  <button class="btn btn-outline btn-sm" on:click={doSubdivideBeats} disabled={$taskRunning || subdividing || resyncing} title={$t('writing.subdivide.tip')}>
                    {#if subdividing}<span class="loading loading-spinner loading-xs"></span>{/if}
                    {$t('writing.subdivide.btn')}
                  </button>
                {/if}
                <div class="flex-1"></div>
                <div class="join">
                  <button class="btn btn-ghost btn-xs join-item" on:click={prevChapter} disabled={$selectedChapter <= 0}>{$t('writing.btn.prev')}</button>
                  <button class="btn btn-ghost btn-xs join-item" on:click={nextChapter} disabled={$selectedChapter >= chapters.length - 1}>{$t('writing.btn.next')}</button>
                </div>
              </div>

              <!-- Bestseller critic over THIS drafted chapter: same card as the
                   outline analysis, but the editor knows it is reviewing one
                   chapter and its suggestions carry trope-unit interventions. -->
              {#if hasContent && ch.status !== 'writing'}
                <div id="CardSection_ChapterAnalysis_{ch.num}" class="pt-1">
                  <BestsellerCritiqueCard
                    kind="chapter"
                    report={chapterAnalysis}
                    chapterNum={ch.num}
                    disabled={$taskRunning}
                    hasTargets={true}
                    scrollId="CardSection_ChapterAnalysis_" + ch.num />
                </div>
              {/if}

              {#if showRevise}
                <div id="CardSection_ChapterRevise" class="bg-base-100 rounded-lg p-3 space-y-2">
                    {#if showKnowledge}
                      <fieldset id="ChapterKnowledgePicker" class="border border-base-content/20 rounded-lg p-3 space-y-3" disabled={$taskRunning || savingKnowledge}>
                        <legend>{$t('writing.knowledge.title')}</legend>
                        <p class="text-sm">{$t('writing.knowledge.hint')}</p>
                        <label class="block text-sm">{$t('config.wv.name')}<input class="input input-sm w-full" bind:value={knowledgeName} /></label>
                        <label class="block text-sm">{$t('config.wv.description')}<textarea class="textarea w-full" rows="4" bind:value={knowledgeDescription}></textarea></label>
                        <label class="block text-sm">{$t('config.wv.tags')}<input class="input input-sm w-full" bind:value={knowledgeTags} /></label>
                        <div class="flex gap-2 flex-wrap">
                          <button class="btn btn-outline btn-sm" on:click={() => saveKnowledge()} disabled={!knowledgeName.trim() || !knowledgeDescription.trim()}>{$t('common.save')}</button>
                          <button class="btn btn-primary btn-sm" on:click={() => saveKnowledge(true)} disabled={!knowledgeName.trim() || !knowledgeDescription.trim()}>{$t('writing.knowledge.saveRevise')}</button>
                        </div>
                        <p class="text-sm">{$t('writing.knowledge.select')}</p>
                        <div class="max-h-48 overflow-y-auto space-y-2">
                          {#each ($settings?.worldview || []) as entry (entry.id)}
                            <label class="flex items-start gap-2 text-sm"><input type="checkbox" class="checkbox checkbox-sm" value={entry.id} bind:group={selectedSettings} /><span>{entry.name}</span></label>
                            {#if selectedSettings.includes(entry.id)}<p class="text-sm whitespace-pre-wrap pl-6">{entry.description}</p>{/if}
                          {/each}
                        </div>
                      </fieldset>
                    {/if}
                  <textarea
                    class="textarea textarea-sm w-full text-sm"
                    bind:value={reviseFeedback}
                    bind:this={reviseTextareaEl}
                    placeholder={$t('writing.revise.placeholder')}
                    disabled={$taskRunning}
                  ></textarea>
                  <div class="flex justify-between items-center gap-2 flex-wrap">
                    <span class="text-xs text-base-content/65">
                        {#if selectedSettings.length || !(isCurrent && ch.status === 'review')}
                        {$t('writing.revise.hintTargeted')}
                      {:else}
                        {$t('writing.revise.hintCurrent')}
                      {/if}
                      <span class="ml-1 text-base-content/30">· {$t('writing.revise.quoteHint')}</span>
                    </span>
                    <div class="flex gap-2">
                      <button class="btn btn-ghost btn-xs" on:click={() => { showRevise = false; reviseFeedback = ''; selectedSettings = []; showKnowledge = false; }} disabled={savingKnowledge}>{$t('common.cancel')}</button>
                      <button class="btn btn-primary btn-xs" on:click={() => doRevise()} disabled={$taskRunning || savingKnowledge || (!reviseFeedback.trim() && !selectedSettings.length)}>{selectedSettings.length ? $t('writing.knowledge.revise') : $t('writing.revise.submit')}</button>
                    </div>
                  </div>
                </div>
              {/if}
            </div>
          </div>

          <!-- Sticky sheet Save button — rendered OUTSIDE CardSection_ChapterReader.
               Behavior: while the reader is in view it sits at the bottom of the
               card flow (normal position via sticky offset); when the user scrolls
               past the top of the reader it disappears; when they reach the end of
               the reader area it settles back into its normal inline position.
               It only appears when there are unsaved edits on the sheet. -->
          <div bind:this={saveBtnSentinel} class="h-0"></div>
          {#if ch && chapterBlocks.length > 0 && !isStreamingThis && (sheetDirty || sheetFocused || readerAbove)}
            <div class="save-sheet-bar z-40 flex justify-end px-2 py-1.5 {readerAbove ? 'fixed right-4 bottom-4 shadow-xl rounded-full' : 'sticky bottom-3'}">
              <button class="btn btn-primary btn-sm shadow-lg"
                      title={sheetDirty ? $t('writing.sheet.dirtyTip') : ''}
                      disabled={!sheetDirty || sheetSaving || $taskRunning}
                      on:click={() => saveSheet()}>
                {#if sheetSaving}<span class="loading loading-spinner loading-xs"></span>{/if}
                {$t(sheetSaving ? 'writing.sheet.saving' : 'writing.sheet.save')}
              </button>
            </div>
          {/if}
        {:else}
          <div class="text-center py-16 text-base-content/65 text-base">{$t('writing.emptySelection')}</div>
        {/if}
      </div>
    </div><!-- /WritingWorkspace (Tab 2) -->
    {/if}

    <!-- ═════════ TAB 3 · REVIEW & ACCEPT ═════════
         All consistency-facts information (KnowledgePanel with its own
         facts binding — reviewFacts — so the prose chips of tab 2 never
         see it) plus the button that accepts the chapter. -->
    {#if writingTab === 3}
      <div id="CardSection_ReviewAccept" class="rounded-xl bg-white shadow-subtle mt-3 min-w-0 p-8">
        <!-- HEADER: big clear title + status pill (mirrors the Scene matrix
             inline card of tab 1 — same Notion/Revolut document style). -->
        <div class="flex items-center gap-3 flex-wrap px-0 pt-0 pb-4 border-b border-neutral-100">
          <h3 class="text-lg font-semibold text-neutral-900 flex-1 leading-tight">
            {$t('writing.chapter.title', { num: ch?.num ?? '-', title: ch?.title ?? '' })}
          </h3>
          {#if ch}
            {#if ch.status === 'accepted'}
              <span class="inline-flex items-center gap-1 rounded-full bg-emerald-50 border border-emerald-200 px-2.5 py-0.5 text-[11px] font-medium text-emerald-700">✓ {$t('writing.tab.accepted')}</span>
            {:else if ch.status === 'review'}
              <span class="inline-flex items-center gap-1 rounded-full bg-amber-50 border border-amber-200 px-2.5 py-0.5 text-[11px] font-medium text-amber-700">• {$t('writing.tab.reviewBadge')}</span>
            {:else}
              <span class="inline-flex items-center rounded-full bg-neutral-100 border border-neutral-200 px-2.5 py-0.5 text-[11px] font-medium text-neutral-600">{statusMeta[ch.status]?.label || ch.status}</span>
            {/if}
          {/if}
        </div>

        {#if ch}
          <!-- CHAPTER SUMMARY: its own Notion-style section with a muted
               uppercase label, like every other block of the inline cards. -->
          {#if ch.summary}
            <div class="px-0 pt-5">
              <div class="text-[10px] font-semibold uppercase tracking-wide text-neutral-400 mb-1.5">{$t('writing.chapter.summary')}</div>
              <p class="text-sm leading-relaxed text-neutral-700 whitespace-pre-wrap bg-neutral-50 border border-neutral-100 rounded-lg px-3.5 py-3">{ch.summary}</p>
            </div>
          {/if}

          <!-- CONSISTENCY FACTS section -->
          <div class="px-0 pt-5">
            <div class="flex items-center gap-2 mb-1.5">
              <span class="text-[10px] font-semibold uppercase tracking-wide text-neutral-400">{$t('writing.tab.factsTitle')}</span>
              <span class="rounded-full bg-neutral-100 border border-neutral-200 px-2 py-0.5 text-[10px] font-medium text-neutral-600">{$t('facts.count', { count: reviewFacts.length })}</span>
            </div>
            <KnowledgePanel chapterNum={ch.num} bind:facts={reviewFacts} bind:activeFact bind:expanded={knowledgeExpanded} on:jump={e => jumpToFact(e.detail)} />
            {#if returnRef}<button class="btn btn-ghost btn-xs self-start text-neutral-500" on:click={() => jumpToFact(returnRef, true)}>{$t('facts.return')}</button>{/if}
          </div>

          <!-- FOOTER: accept action, hairline-separated like the scene-plan
               editor footer. -->
          <div class="flex justify-end items-center gap-2 px-0 pt-5 mt-5 border-t border-neutral-100">
            {#if ch.status === 'review' && isCurrent}
              <button class="btn btn-success btn-sm rounded-lg" on:click={doConfirm} disabled={$taskRunning}>{$t('writing.btn.confirm')}</button>
            {/if}
          </div>
        {:else}
          <div class="px-0 py-10 text-center text-sm text-neutral-400">{$t('writing.emptySelection')}</div>
        {/if}
      </div>
    {/if}
  </div><!-- /WritingPageContainer -->
{/if}

<style>
  /* Trope-issue dots — small colored markers painted on story blocks (and on
     the "Scene N starts here" chip) when the fact-check and/or the bestseller
     critic found something to repair in that trope unit. They are rendered as
     the `trigger` slot of InfoTip, so clicking one opens the same fixed-
     position popover style used by the SmartSelect/SmartCombobox summary tips,
     listing the exact issue wordings for that unit. */
  .trope-dot {
    display: inline-block;
    width: 9px;
    height: 9px;
    border-radius: 9999px;
    cursor: help;
    flex-shrink: 0;
    transition: transform 0.12s ease, box-shadow 0.12s ease;
  }
  .trope-dot:hover {
    transform: scale(1.35);
  }
  .trope-dot-fact {
    background: var(--fallback-error, oklch(var(--error)));
    box-shadow: 0 0 0 2px color-mix(in oklab, var(--fallback-er, oklch(var(--er))) 25%, transparent);
  }
  .trope-dot-critic {
    background: var(--fallback-secondary, oklch(var(--secondary)));
    box-shadow: 0 0 0 2px color-mix(in oklab, var(--fallback-sc, oklch(var(--sc))) 25%, transparent);
  }

  /* —— Continuous sheet (Word-like) styles ——————————————— */
  /* Chips between scenes/tropes are almost imperceptible until hovered and
     can never be captured by text selection (request 7). */
  .sheet-chip-row {
    user-select: none;
    -webkit-user-select: none;
  }
  .chip-scene-or-trope {
    opacity: 0.35;
    transition: opacity 0.15s ease;
    user-select: none;
    -webkit-user-select: none;
  }
  .chip-scene-or-trope:hover { opacity: 1; }
  /* The hover-revealed "＋ add subscene" action lives OUTSIDE the chip (as a
     sibling pill in the chip row); it appears when hovering anywhere on the
     row, and must stay fully opaque even while the chip sits at its dim idle
     opacity. Scoped via :global because group/chiprow is a Tailwind class. */
  .chip-add-before {
    user-select: none;
    -webkit-user-select: none;
    /* Zero footprint: out of flow + invisible to hit-testing while idle, so it
       never overlaps the centered chip nor shifts the row layout. */
    position: absolute;
    pointer-events: none;
    visibility: hidden;
    opacity: 0;
    z-index: 40;
  }
  /* Ghost variant: taken OUT of the flex flow. The pill is pinned to the
     RIGHT EDGE of the row (never over the centered chip) and lives absolutely
     positioned at right:8px — so it occupies ZERO space in the layout until
     hovered and can never push the centered chip sideways or overlap it.
     visibility:hidden (not just opacity:0) also removes it from hit-testing;
     pointer-events re-activate on row hover. */
  .chip-add-before--ghost {
    left: auto;
    /* Pinned to the far-right EDGE of the row (well clear of the centered
       chip, even when the chip is wide) and vertically centered. */
    right: 0;
    top: 50%;
    transform: translateY(-50%);
  }
  :global(.group\/chiprow:hover) .chip-add-before,
  .chip-add-before:focus-visible { opacity: 1; }
  :global(.group\/chiprow:hover) .chip-add-before--ghost { pointer-events: auto; visibility: visible; }
  /* SCENE chips are real structural markers — unlike the subtle subscene
     chips they must ALWAYS be fully visible (never dimmed at idle). */
  .chip-always-on { opacity: 1 !important; }
  /* Small "i" trigger that lives OUTSIDE a scene chip and opens the black
     SmartSelect-style popover with the scene title + description. Only scene
     chips get one; subscene chips have no tooltip of any kind. */
  .scene-desc-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 14px;
    height: 14px;
    border-radius: 9999px;
    border: 1px solid color-mix(in oklab, currentColor 30%, transparent);
    font-size: 9px;
    font-weight: 700;
    line-height: 1;
    color: var(--fallback-bc, oklch(var(--bc)));
    opacity: 0.55;
    cursor: help;
    user-select: none;
    -webkit-user-select: none;
    transition: opacity 0.12s ease, border-color 0.12s ease;
  }
  .scene-desc-icon:hover { opacity: 1; border-color: color-mix(in oklab, currentColor 60%, transparent); }
  /* Chapter-head row (title + status badge) inside the reader card: roomy
     Notion-like margins so the header breathes. */
  .chapter-head-row { margin: 1.5rem; }
  /* Collapsible outline/<details> in the chapter-writing area: same breathing
     margin as the head row + soft rounded corners. */
  .chapter-outline-fold { margin: 1.5rem; border-radius: 0.5rem; }
  .sheet-chip-text {
    user-select: none;
    -webkit-user-select: none;
  }
  /* "Key fact #N" chips living inside the scene/trope marker row. They stay
     subtle like the markers but brighten on hover; clicking jumps to the
     fact's source block (same behavior as the paragraph-level badges). */
  .keyfact-chip {
    user-select: none;
    -webkit-user-select: none;
    font-size: 9px;
    line-height: 1.2;
    font-weight: 600;
    padding: 1px 6px;
    border-radius: 9999px;
    background: color-mix(in oklab, var(--fallback-warning, oklch(var(--warning))) 18%, transparent);
    border: 1px solid color-mix(in oklab, var(--fallback-warning, oklch(var(--warning))) 45%, transparent);
    color: var(--fallback-warning-foreground, oklch(var(--warning)));
    opacity: 0.75;
    transition: opacity 0.15s ease, transform 0.12s ease;
    cursor: pointer;
  }
  .keyfact-chip:hover { opacity: 1; transform: scale(1.08); }
  #ChapterSheetEditor :global(.iv-mark) {
    /* Per-suggestion tint comes from the inline style (ivMarkStyle): a solid
       highlighter background + matching underline, one color per critic
       intervention. The theme is NOT hardcoded here — the popup/modal read
       the same ivColor() hash, so card theme always equals its highlight.
       These rules only add hover affordance on top of the inline tint. */
    color: inherit;
    padding: 0 1px;
    border-radius: 2px;
    cursor: pointer;
    transition: filter 0.12s ease, box-shadow 0.12s ease;
  }
  #ChapterSheetEditor :global(.iv-mark:hover) {
    filter: brightness(0.96) saturate(1.4);
    box-shadow: 0 0 0 2px color-mix(in oklab, currentColor 8%, transparent);
  }
  /* Small "×N" badge shown right after a fused mark so the reader knows the
     beat carries several critic suggestions (click opens the tabbed popup). */
  #ChapterSheetEditor .iv-count {
    font-size: 9px;
    font-weight: 700;
    line-height: 1;
    margin-left: 1px;
    vertical-align: super;
    user-select: none;
    -webkit-user-select: none;
  }
    .save-sheet-bar.sticky {
    bottom: 0.75rem;
  }
  /* The continuous sheet must never show a focus ring — it is always editable. */
  #ChapterSheetEditor,
  #ChapterSheetEditor:focus,
  #ChapterSheetEditor:focus-visible,
  #ChapterSheetEditor [data-bid]:focus,
  #ChapterSheetEditor [data-bid]:focus-visible {
    outline: none !important;
    box-shadow: none !important;
  }

  /* —— Writing step tabs (Scene matrix / Chapter writing / Review & accept) —
     Ultra-minimalist Notion x Revolut stepper: cubic-bezier transitions on
     the tab cards + progress bar, and a hairline shadow for the active card
     (the project uses Tailwind v4 without a config file, so the sample's
     `shadow-subtle` utility is defined here instead). */
  .step-transition {
    transition: all 0.25s cubic-bezier(0.16, 1, 0.3, 1);
  }
  .shadow-subtle {
    box-shadow: 0 1px 2px 0 rgba(0, 0, 0, 0.03);
  }
</style>
