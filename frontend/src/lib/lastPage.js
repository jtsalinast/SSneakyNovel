// Last visited workspace page per project. Kept in localStorage so opening a
// project resumes where the author left off; a missing/blocked store is not an
// error (private mode, quota).
const KEY = 'lastPage:';

export function readLastPage(project) {
  if (!project) return null;
  try { return localStorage.getItem(KEY + project); } catch (_) { return null; }
}

export function writeLastPage(project, page) {
  if (!project || !page) return;
  try { localStorage.setItem(KEY + project, page); } catch (_) {}
}

// The ribbon step inside a page is remembered too, so reopening a project
// returns to the same subsection (Historic Memory vs Progression, etc.).
const STEP_KEY = 'lastStep:';

export function readLastStep(project, page) {
  if (!project || !page) return null;
  try { return localStorage.getItem(STEP_KEY + project + ':' + page); } catch (_) { return null; }
}

export function writeLastStep(project, page, step) {
  if (!project || !page || !step) return;
  try { localStorage.setItem(STEP_KEY + project + ':' + page, step); } catch (_) {}
}
