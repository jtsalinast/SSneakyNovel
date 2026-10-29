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
